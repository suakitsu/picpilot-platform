package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type PromptTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scene     string    `json:"scene"`
	Content   string    `json:"content"`
	Enabled   bool      `json:"enabled"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type AdminPromptInput struct {
	Name    *string `json:"name"`
	Scene   *string `json:"scene"`
	Content *string `json:"content"`
	Enabled *bool   `json:"enabled"`
	Version *int64  `json:"version,omitempty"`
}

const adminPromptColumns = `id,name,scene,content,enabled,version,created_at,updated_at`

func scanAdminPrompt(row pgx.Row) (PromptTemplate, error) {
	var p PromptTemplate
	err := row.Scan(&p.ID, &p.Name, &p.Scene, &p.Content, &p.Enabled, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, normalizeError(err)
}

func (s *Store) AdminPrompts(ctx context.Context, actor string, f AdminFilter) (AdminPage, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return AdminPage{}, err
	}
	defer tx.Rollback(context.Background())
	q := adminWhere{}
	q.search([]string{"p.id", "p.name", "p.scene"}, f.Keyword)
	q.exact("p.scene", f.Feature)
	if f.Status == "enabled" {
		q.parts = append(q.parts, "p.enabled")
	} else if f.Status == "disabled" {
		q.parts = append(q.parts, "NOT p.enabled")
	} else if f.Status != "" {
		return AdminPage{}, ErrConflict
	}
	return adminPaged(ctx, tx, f, "admin_prompt_templates p", "to_jsonb(p)", "p.updated_at DESC,p.id DESC", q)
}
func (s *Store) AdminSavePrompt(ctx context.Context, actor, id, requestID string, in AdminPromptInput) (PromptTemplate, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return PromptTemplate{}, err
	}
	defer tx.Rollback(context.Background())
	p := PromptTemplate{ID: id, Enabled: true, Version: 1}
	create := id == ""
	if !create {
		p, err = scanAdminPrompt(tx.QueryRow(ctx, "SELECT "+adminPromptColumns+" FROM admin_prompt_templates WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return p, err
		}
		if in.Version != nil && *in.Version != p.Version {
			return p, ErrConflict
		}
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Scene != nil {
		p.Scene = strings.TrimSpace(*in.Scene)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.Content != nil && *in.Content != p.Content {
		p.Content = *in.Content
		if !create {
			p.Version++
		}
	}
	if p.Name == "" || len([]rune(p.Name)) > 160 || !adminLabel.MatchString(p.Scene) || len(p.Scene) > 80 || strings.TrimSpace(p.Content) == "" || len([]rune(p.Content)) > 40000 || strings.ContainsRune(p.Name+p.Content, 0) {
		return p, ErrConflict
	}
	action := "prompt_template_update"
	if create {
		p.ID = security.NewID("prompt")
		action = "prompt_template_create"
		p, err = scanAdminPrompt(tx.QueryRow(ctx, `INSERT INTO admin_prompt_templates(id,name,scene,content,enabled) VALUES($1,$2,$3,$4,$5) RETURNING `+adminPromptColumns, p.ID, p.Name, p.Scene, p.Content, p.Enabled))
	} else {
		p, err = scanAdminPrompt(tx.QueryRow(ctx, `UPDATE admin_prompt_templates SET name=$2,scene=$3,content=$4,enabled=$5,version=$6,updated_at=now() WHERE id=$1 RETURNING `+adminPromptColumns, p.ID, p.Name, p.Scene, p.Content, p.Enabled, p.Version))
	}
	if err != nil {
		return p, err
	}
	if err = adminAudit(ctx, tx, actor, action, "prompt_template", p.ID, requestID, map[string]any{"scene": p.Scene, "version": p.Version, "enabled": p.Enabled}); err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

// ActivePrompt is a trusted runtime hook, not a public prompt-disclosure API.
func (s *Store) ActivePrompt(ctx context.Context, scene string) (PromptTemplate, error) {
	if s.Edition != config.Platform {
		return PromptTemplate{}, ErrNotFound
	}
	return scanAdminPrompt(s.Pool.QueryRow(ctx, "SELECT "+adminPromptColumns+" FROM admin_prompt_templates WHERE scene=$1 AND enabled ORDER BY updated_at DESC,id DESC LIMIT 1", scene))
}

type ImageConfig struct {
	ID             string    `json:"id"`
	ImageType      string    `json:"image_type"`
	ImageName      string    `json:"image_name"`
	Enabled        bool      `json:"enabled"`
	CreditsCost    int64     `json:"credits_cost"`
	DefaultModel   string    `json:"default_model"`
	FallbackModel  string    `json:"fallback_model"`
	DefaultSize    string    `json:"default_size"`
	RequiredFields string    `json:"required_fields"`
	SortOrder      int       `json:"sort_order"`
	Version        int64     `json:"version"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type AdminImageConfigInput struct {
	Enabled        *bool           `json:"enabled"`
	CreditsCost    *int64          `json:"credits_cost"`
	DefaultModel   *string         `json:"default_model"`
	FallbackModel  *string         `json:"fallback_model"`
	DefaultSize    *string         `json:"default_size"`
	RequiredFields json.RawMessage `json:"required_fields"`
	Version        *int64          `json:"version,omitempty"`
}

func scanImageConfig(row pgx.Row) (ImageConfig, error) {
	var raw []byte
	var c ImageConfig
	if err := row.Scan(&raw); err != nil {
		return c, normalizeError(err)
	}
	err := json.Unmarshal(raw, &c)
	return c, err
}
func (s *Store) AdminImageConfigs(ctx context.Context, actor string) ([]map[string]any, error) {
	tx, err := s.adminRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	return adminJSONRows(ctx, tx, `SELECT to_jsonb(c) FROM admin_image_configs c ORDER BY sort_order,id`)
}
func (s *Store) AdminSaveImageConfig(ctx context.Context, actor, id, requestID string, in AdminImageConfigInput) (ImageConfig, error) {
	tx, err := s.adminBegin(ctx, actor)
	if err != nil {
		return ImageConfig{}, err
	}
	defer tx.Rollback(context.Background())
	c, err := scanImageConfig(tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM admin_image_configs c WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return c, err
	}
	if in.Version != nil && *in.Version != c.Version {
		return c, ErrConflict
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if in.CreditsCost != nil {
		c.CreditsCost = *in.CreditsCost
	}
	if in.DefaultModel != nil {
		c.DefaultModel = strings.TrimSpace(*in.DefaultModel)
	}
	if in.FallbackModel != nil {
		c.FallbackModel = strings.TrimSpace(*in.FallbackModel)
	}
	if in.DefaultSize != nil {
		c.DefaultSize = *in.DefaultSize
	}
	if len(in.RequiredFields) > 0 {
		if string(in.RequiredFields) == "null" || json.Unmarshal(in.RequiredFields, &c.RequiredFields) != nil {
			return c, ErrConflict
		}
	}
	if c.CreditsCost < 0 || c.CreditsCost > 1000000 || len(c.DefaultModel) > 160 || len(c.FallbackModel) > 160 || strings.ContainsAny(c.DefaultModel+c.FallbackModel, "\r\n\x00") || len([]rune(c.RequiredFields)) > 1000 || strings.ContainsRune(c.RequiredFields, 0) {
		return c, ErrConflict
	}
	if !validAdminImageSize(c.DefaultSize) {
		return c, ErrConflict
	}
	// These are model IDs, never provider IDs. Disabled providers may be staged
	// here; runtime dispatch still applies provider availability and health rules.
	for _, model := range []string{c.DefaultModel, c.FallbackModel} {
		if model == "" {
			continue
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM providers WHERE owner_scope='platform' AND capability='image' AND model=$1)`, model).Scan(&exists); err != nil {
			return c, err
		}
		if !exists {
			return c, ErrConflict
		}
	}
	c, err = scanImageConfig(tx.QueryRow(ctx, `UPDATE admin_image_configs c SET enabled=$2,credits_cost=$3,default_model=$4,fallback_model=$5,default_size=$6,required_fields=$7,version=version+1,updated_at=now() WHERE id=$1 RETURNING to_jsonb(c)`, id, c.Enabled, c.CreditsCost, c.DefaultModel, c.FallbackModel, c.DefaultSize, c.RequiredFields))
	if err != nil {
		return c, err
	}
	if err = adminAudit(ctx, tx, actor, "image_config_update", "image_config", id, requestID, map[string]any{"enabled": c.Enabled, "version": c.Version, "credits_cost": c.CreditsCost}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func validAdminImageSize(s string) bool {
	for _, v := range []string{"1024x1024", "2048x2048", "4096x4096", "1040x832", "720x1280", "1280x720", "1024x768", "1008x672", "832x1040", "768x1024", "672x1008", "1344x576", "2080x1664", "1152x2048", "2048x1152", "2048x1536", "2016x1344", "1664x2080", "1536x2048", "1344x2016", "2016x864", "2880x2880", "3200x2560", "2160x3840", "3840x2160", "3264x2448", "3504x2336", "2560x3200", "2448x3264", "2336x3504", "3696x1584"} {
		if s == v {
			return true
		}
	}
	return false
}
func (s *Store) RuntimeImageConfig(ctx context.Context, kind string) (ImageConfig, error) {
	if s.Edition != config.Platform {
		return ImageConfig{}, ErrNotFound
	}
	return scanImageConfig(s.Pool.QueryRow(ctx, `SELECT to_jsonb(c) FROM admin_image_configs c WHERE image_type=$1`, kind))
}
