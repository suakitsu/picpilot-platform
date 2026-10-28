package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/risk"
	"github.com/suakitsu/picpilot-platform/core/security"
)

var adminLabel = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,160}$`)

func riskConfigTx(ctx context.Context, tx pgx.Tx) (risk.Config, error) {
	c := risk.DefaultConfig()
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT value FROM app_settings WHERE key='risk_control'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if json.Unmarshal(raw, &c) != nil || c.Validate() != nil {
		return c, ErrConflict
	}
	return c, nil
}

func (s *Store) RiskConfig(ctx context.Context) (risk.Config, error) {
	if s.Edition == config.Personal {
		c := risk.DefaultConfig()
		c.Enabled = false
		return c, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return risk.Config{}, err
	}
	defer tx.Rollback(context.Background())
	return riskConfigTx(ctx, tx)
}
func (s *Store) AdminRiskConfig(ctx context.Context, actor string) (risk.Config, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return risk.Config{}, err
	}
	defer tx.Rollback(context.Background())
	return riskConfigTx(ctx, tx)
}

type AdminRiskPatch struct {
	Enabled      *bool     `json:"enabled"`
	BanThreshold *int      `json:"ban_threshold"`
	BlockLevels  *[]string `json:"block_levels"`
	WarnLevels   *[]string `json:"warn_levels"`
}

func saveRiskConfig(ctx context.Context, tx pgx.Tx, actor, requestID string, c risk.Config) error {
	if c.Validate() != nil {
		return ErrConflict
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES('risk_control',$1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,version=app_settings.version+1,updated_by=EXCLUDED.updated_by,updated_at=now()`, raw, actor); err != nil {
		return err
	}
	return adminAudit(ctx, tx, actor, "risk_config_update", "setting", "risk_control", requestID, map[string]any{"enabled": c.Enabled, "ban_threshold": c.BanThreshold})
}
func (s *Store) AdminUpdateRiskConfig(ctx context.Context, actor, requestID string, p AdminRiskPatch) (risk.Config, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return risk.Config{}, err
	}
	defer tx.Rollback(context.Background())
	c, err := riskConfigTx(ctx, tx)
	if err != nil {
		return c, err
	}
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	if p.BanThreshold != nil {
		c.BanThreshold = *p.BanThreshold
	}
	if p.BlockLevels != nil {
		c.BlockLevels = *p.BlockLevels
	}
	if p.WarnLevels != nil {
		c.WarnLevels = *p.WarnLevels
	}
	if err = saveRiskConfig(ctx, tx, actor, requestID, c); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Store) AdminRiskKeyword(ctx context.Context, actor, requestID, level, keyword string, remove bool) (risk.Config, error) {
	if !risk.ValidLevel(level) {
		return risk.Config{}, ErrConflict
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || len([]rune(keyword)) > 120 || strings.ContainsRune(keyword, 0) {
		return risk.Config{}, ErrConflict
	}
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return risk.Config{}, err
	}
	defer tx.Rollback(context.Background())
	c, err := riskConfigTx(ctx, tx)
	if err != nil {
		return c, err
	}
	next := []string{}
	exists := false
	for _, v := range c.Keywords[level] {
		if risk.Normalize(v) == risk.Normalize(keyword) {
			exists = true
			if remove {
				continue
			}
		}
		next = append(next, v)
	}
	if !remove && !exists {
		next = append(next, keyword)
	}
	c.Keywords[level] = next
	if err = saveRiskConfig(ctx, tx, actor, requestID, c); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func (s *Store) RiskUserAllowed(ctx context.Context, user string) error {
	var status string
	err := s.Pool.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, user).Scan(&status)
	if err != nil {
		return normalizeError(err)
	}
	if status != "active" {
		return risk.ErrAccountRestricted
	}
	return nil
}

func (s *Store) RecordRiskHit(ctx context.Context, e risk.Event, threshold int) (risk.Decision, error) {
	d := risk.Decision{Action: e.Action, Level: e.Level}
	if s.Edition != config.Platform || threshold < 1 || threshold > 100 || !risk.ValidLevel(e.Level) || !adminLabel.MatchString(e.Feature) || len(e.RequestKey) < 8 || len(e.RequestKey) > 160 || len(e.Keyword) == 0 || len([]rune(e.Keyword)) > 120 || strings.ContainsRune(e.Keyword, 0) {
		return d, ErrConflict
	}
	if e.Source != "user_input" && e.Source != "provider_error" {
		return d, ErrConflict
	}
	if e.Action != "blocked" && e.Action != "warned" && e.Action != "recorded" {
		return d, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer tx.Rollback(context.Background())
	var role, status string
	if err = tx.QueryRow(ctx, "SELECT role,status FROM users WHERE id=$1 FOR UPDATE", e.UserID).Scan(&role, &status); err != nil {
		return d, normalizeError(err)
	}
	if e.ProjectID != "" {
		var owns bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)", e.ProjectID, e.UserID).Scan(&owns); err != nil {
			return d, err
		}
		if !owns {
			return d, ErrNotFound
		}
	}
	if e.TaskID != "" {
		var owns bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND user_id=$2 AND ($3='' OR project_id=$3))", e.TaskID, e.UserID, e.ProjectID).Scan(&owns); err != nil {
			return d, err
		}
		if !owns {
			return d, ErrNotFound
		}
	}
	sum := sha256.Sum256([]byte(e.RequestKey))
	key := hex.EncodeToString(sum[:])
	result, err := tx.Exec(ctx, `INSERT INTO admin_risk_events(id,user_id,project_id,task_id,request_key_hash,feature,risk_level,keyword,source,action) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,$9,$10) ON CONFLICT(user_id,request_key_hash,source) DO NOTHING`, security.NewID("risk"), e.UserID, e.ProjectID, e.TaskID, key, e.Feature, e.Level, e.Keyword, e.Source, e.Action)
	if err != nil {
		return d, err
	}
	d.Duplicate = result.RowsAffected() == 0
	if d.Duplicate {
		var previous risk.Event
		err = tx.QueryRow(ctx, `SELECT COALESCE(project_id,''),COALESCE(task_id,''),feature,risk_level,keyword,action FROM admin_risk_events WHERE user_id=$1 AND request_key_hash=$2 AND source=$3`, e.UserID, key, e.Source).Scan(&previous.ProjectID, &previous.TaskID, &previous.Feature, &previous.Level, &previous.Keyword, &previous.Action)
		if err != nil {
			return d, err
		}
		if previous.ProjectID != e.ProjectID || previous.TaskID != e.TaskID || previous.Feature != e.Feature || previous.Level != e.Level || previous.Keyword != e.Keyword || previous.Action != e.Action {
			return d, ErrConflict
		}
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM admin_risk_events WHERE user_id=$1 AND cleared_at IS NULL`, e.UserID).Scan(&d.Count); err != nil {
		return d, err
	}
	// Automatic restrictions can never remove an administrator or personal owner.
	if !d.Duplicate && d.Count >= int64(threshold) && role == "user" && status == "active" {
		if _, err = tx.Exec(ctx, `UPDATE users SET status='disabled',auth_version=auth_version+1,metadata=metadata||'{"risk_restricted":true}'::jsonb,updated_at=now() WHERE id=$1`, e.UserID); err != nil {
			return d, err
		}
		status = "disabled"
	}
	d.Restricted = status != "active"
	return d, tx.Commit(ctx)
}

func (s *Store) AdminClearRiskEvents(ctx context.Context, actor, user, requestID string) (int64, string, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(context.Background())
	u, err := adminTarget(ctx, tx, actor, user)
	if err != nil {
		return 0, "", err
	}
	res, err := tx.Exec(ctx, `UPDATE admin_risk_events SET cleared_at=now() WHERE user_id=$1 AND cleared_at IS NULL`, user)
	if err != nil {
		return 0, "", err
	}
	var status string
	err = tx.QueryRow(ctx, `UPDATE users SET status=CASE WHEN metadata->'risk_restricted'='true'::jsonb THEN 'active' ELSE status END,metadata=metadata-'risk_restricted',auth_version=auth_version+1,updated_at=now() WHERE id=$1 RETURNING status`, user).Scan(&status)
	if err != nil {
		return 0, "", err
	}
	if err = adminAudit(ctx, tx, actor, "risk_events_clear", "user", user, requestID, map[string]any{"cleared": res.RowsAffected(), "before_status": u.Status, "status": status}); err != nil {
		return 0, "", err
	}
	return res.RowsAffected(), status, tx.Commit(ctx)
}

func (s *Store) AdminRiskEvents(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"e.id", "e.keyword", "e.feature", "e.source"}, f.Keyword)
	q.search([]string{"u.id", "u.email", "u.nickname"}, f.UserKeyword)
	q.exact("e.user_id", f.UserID)
	q.exact("e.risk_level", f.RiskLevel)
	q.exact("e.source", f.Source)
	q.exact("e.action", f.Action)
	q.exact("e.feature", f.Feature)
	q.dates("e.created_at", f)
	if f.Cleared != nil {
		q.add("(e.cleared_at IS NOT NULL)=?", *f.Cleared)
	}
	return adminPaged(ctx, tx, f, "admin_risk_events e JOIN users u ON u.id=e.user_id", `jsonb_build_object('id',e.id,'user_id',e.user_id,'user_account',u.email,'user_email_masked',`+adminMaskedEmail+`,'project_id',e.project_id,'task_id',e.task_id,'feature',e.feature,'risk_level',e.risk_level,'keyword',e.keyword,'matched_text',NULL,'source',e.source,'action',e.action,'cleared',e.cleared_at IS NOT NULL,'created_at',e.created_at)`, "e.created_at DESC,e.id DESC", q)
}
