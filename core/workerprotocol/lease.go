package workerprotocol

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

type Status string

const (
	Queued    Status = "queued"
	Leased    Status = "leased"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
	Expired   Status = "expired"
)

type OfflineAction string

const (
	Queue    OfflineAction = "queue"
	Fallback OfflineAction = "fallback"
)

// Job is an internal snapshot, never an HTTP response. No raw lease or token
// is retained. The parent owns business-task links separately from wire DTOs.
type Job struct {
	JobID             string
	ProviderID        string
	Status            Status
	Capability        Capability
	Protocol          Protocol
	Model             string
	Request           json.RawMessage
	InputFiles        []InputFile
	OfflineAction     OfflineAction
	QueuedAt          time.Time
	LeasedAt          time.Time
	CompletedAt       time.Time
	WorkerID          string
	LeaseHash         string `json:"-"`
	PreviousLeaseHash string `json:"-"`
	LeaseUntil        time.Time
	RenewAfter        time.Time
	GraceUntil        time.Time
	Attempt           int
	CompletionKey     string
	CompletionHash    string
	ResultJSON        json.RawMessage
	ErrorCode         string
	ErrorMessage      string
	Artifacts         []ArtifactRecord
}
type ArtifactRecord struct {
	JobID          string
	WorkerID       string
	LeaseHash      string `json:"-"`
	IdempotencyKey string
	Response       ArtifactResponse
}

func cloneJob(j Job) Job {
	j.Request = bytes.Clone(j.Request)
	j.ResultJSON = bytes.Clone(j.ResultJSON)
	j.InputFiles = slices.Clone(j.InputFiles)
	j.Artifacts = slices.Clone(j.Artifacts)
	return j
}

type LeasePolicy struct {
	LeaseSeconds int
	GraceSeconds int
}

func DefaultLeasePolicy() LeasePolicy { return LeasePolicy{LeaseSeconds: 120, GraceSeconds: 15} }
func (p LeasePolicy) Validate() error {
	if p.LeaseSeconds < 30 || p.LeaseSeconds > 1800 || p.GraceSeconds < 5 || p.GraceSeconds > 120 {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return nil
}
func deadlines(now time.Time, p LeasePolicy) (time.Time, time.Time, time.Time) {
	lease := now.Add(time.Duration(p.LeaseSeconds) * time.Second)
	return lease, now.Add(time.Duration(p.LeaseSeconds*2/3) * time.Second), lease.Add(time.Duration(p.GraceSeconds) * time.Second)
}

type Capacity struct{ WorkerActive, ProviderActive, WorkerMax, ProviderMax int }

func (c Capacity) Available() bool {
	return c.WorkerActive >= 0 && c.ProviderActive >= 0 && c.WorkerMax >= 1 && c.WorkerMax <= 32 && c.ProviderMax >= 1 && c.ProviderMax <= 32 && c.WorkerActive < c.WorkerMax && c.ProviderActive < c.ProviderMax
}

// Acquire only leases a selected queued job. FIFO selection and capacity
// accounting must occur in the same parent-owned transaction as persistence.
// A fresh high-entropy leaseID must be provided by the parent on every attempt.
func Acquire(j Job, request LeaseRequest, signingSecret []byte, now time.Time, policy LeasePolicy) (Job, ClaimedTask, error) {
	if err := request.Validate(); err != nil {
		return j, ClaimedTask{}, err
	}
	if err := policy.Validate(); err != nil {
		return j, ClaimedTask{}, err
	}
	if now.IsZero() || j.JobID == "" || j.Attempt < 0 || (j.OfflineAction != Queue && j.OfflineAction != Fallback) {
		return j, ClaimedTask{}, NewError("REQUEST_SCHEMA_INVALID")
	}
	if j.Status == Cancelled {
		return j, ClaimedTask{}, NewError("JOB_CANCELLED")
	}
	if j.Status != Queued {
		return j, ClaimedTask{}, NewError("JOB_NOT_LEASED")
	}
	hash, err := LeaseDigest(signingSecret, request.LeaseID)
	if err != nil {
		return j, ClaimedTask{}, err
	}
	if equalConstant(hash, j.PreviousLeaseHash) || equalConstant(hash, j.LeaseHash) {
		return j, ClaimedTask{}, NewError("LEASE_ID_INVALID")
	}
	if err := ValidatePayload(j.Capability, j.Protocol, j.Model, j.Request); err != nil {
		return j, ClaimedTask{}, err
	}
	if err := ValidateInputFiles(j.Protocol, j.JobID, j.InputFiles); err != nil {
		return j, ClaimedTask{}, err
	}
	next := cloneJob(j)
	next.Status = Leased
	next.WorkerID = request.WorkerID
	next.LeaseHash = hash
	next.LeaseUntil, next.RenewAfter, next.GraceUntil = deadlines(now, policy)
	next.LeasedAt = now
	next.Attempt++
	next.ErrorCode = ""
	next.ErrorMessage = ""
	next.CompletionKey = ""
	next.CompletionHash = ""
	next.CompletedAt = time.Time{}
	next.ResultJSON = nil
	task := ClaimedTask{JobID: next.JobID, LeaseID: request.LeaseID, LeaseUntil: next.LeaseUntil, RenewAfter: next.RenewAfter, GraceUntil: next.GraceUntil, Capability: next.Capability, Protocol: next.Protocol, Model: next.Model, Request: bytes.Clone(next.Request), InputFiles: append([]InputFile{}, next.InputFiles...), Attempt: next.Attempt}
	return next, task, nil
}
func NewClaimResponse(task *ClaimedTask, now time.Time, waitedMS int64) (ClaimResponse, error) {
	v := ClaimResponse{Task: task, Empty: task == nil, ServerTime: now, WaitedMS: waitedMS}
	return v, v.Validate()
}

// HardDeadline also accepts legacy snapshots without a separate grace column.
func HardDeadline(j Job) time.Time {
	if j.GraceUntil.IsZero() {
		return j.LeaseUntil
	}
	return j.GraceUntil
}
func IsActive(j Job, now time.Time) bool {
	return !now.IsZero() && j.Status == Leased && now.Before(HardDeadline(j))
}
func verifyIdentity(j Job, request LeaseRequest, secret []byte) error {
	hash, err := LeaseDigest(secret, request.LeaseID)
	if err != nil {
		return err
	}
	if j.WorkerID != request.WorkerID || !equalConstant(j.LeaseHash, hash) {
		return NewError("LEASE_ID_INVALID")
	}
	return nil
}
func VerifyLease(j Job, request LeaseRequest, signingSecret []byte, now time.Time) error {
	if j.JobID == "" {
		return NewError("JOB_NOT_FOUND")
	}
	if j.Status == Cancelled {
		return NewError("JOB_CANCELLED")
	}
	if j.Status == Expired {
		return NewError("LEASE_EXPIRED")
	}
	if j.Status != Leased {
		return NewError("JOB_NOT_LEASED")
	}
	if now.IsZero() || !now.Before(HardDeadline(j)) {
		return NewError("LEASE_EXPIRED")
	}
	if j.WorkerID != request.WorkerID {
		return NewError("WORKER_MISMATCH")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	return verifyIdentity(j, request, signingSecret)
}
func Renew(j Job, request LeaseRequest, signingSecret []byte, now time.Time, policy LeasePolicy) (Job, LeaseResponse, error) {
	if err := policy.Validate(); err != nil {
		return j, LeaseResponse{}, err
	}
	if err := VerifyLease(j, request, signingSecret, now); err != nil {
		return j, LeaseResponse{}, err
	}
	next := cloneJob(j)
	next.LeaseUntil, next.RenewAfter, next.GraceUntil = deadlines(now, policy)
	return next, LeaseResponse{JobID: next.JobID, LeaseUntil: next.LeaseUntil, RenewAfter: next.RenewAfter, GraceUntil: next.GraceUntil}, nil
}
func Expire(j Job, now time.Time) (Job, bool) {
	if now.IsZero() || j.Status != Leased || now.Before(HardDeadline(j)) {
		return j, false
	}
	next := cloneJob(j)
	next.Status = Expired
	next.ErrorCode = "LEASE_EXPIRED"
	next.ErrorMessage = "Worker lease expired"
	return next, true
}
func Requeue(j Job, now time.Time) (Job, bool) {
	if now.IsZero() || j.Status != Expired || j.OfflineAction != Queue {
		return j, false
	}
	next := cloneJob(j)
	next.Status = Queued
	next.PreviousLeaseHash = next.LeaseHash
	next.LeaseHash = ""
	next.WorkerID = ""
	next.LeaseUntil = time.Time{}
	next.RenewAfter = time.Time{}
	next.GraceUntil = time.Time{}
	next.LeasedAt = time.Time{}
	next.ErrorCode = ""
	next.ErrorMessage = ""
	next.CompletionKey = ""
	next.CompletionHash = ""
	next.ResultJSON = nil
	// Keep original queue creation time and attempt count, matching Java FIFO.
	return next, true
}
func Cancel(j Job, now time.Time) (Job, bool) {
	if now.IsZero() || (j.Status != Queued && j.Status != Leased) {
		return j, false
	}
	next := cloneJob(j)
	next.Status = Cancelled
	next.CompletedAt = now
	next.ErrorCode = "BUSINESS_TASK_CANCELLED"
	next.ErrorMessage = "Business image task is no longer active"
	return next, true
}

// ReconcileBusinessTask is called with authoritative business state under lock
// before inputs, artifacts, renewals and terminal actions. It never cancels a
// successfully settled job merely because its business task is now complete.
func ReconcileBusinessTask(j Job, active bool, now time.Time) (Job, bool) {
	if j.Capability == Image && !active {
		return Cancel(j, now)
	}
	return j, false
}

// OfflineFallback cannot start HTTP work for a leased, expired or terminal job.
// The parent must persist the cancellation before starting its provider chain.
func OfflineFallback(j Job, now time.Time, waitSeconds int) (Job, bool) {
	if now.IsZero() || j.QueuedAt.IsZero() || waitSeconds < 1 || waitSeconds > 300 || j.Status != Queued || j.OfflineAction != Fallback || now.Before(j.QueuedAt.Add(time.Duration(waitSeconds)*time.Second)) {
		return j, false
	}
	next := cloneJob(j)
	next.Status = Cancelled
	next.CompletedAt = now
	next.ErrorCode = "LOCAL_WORKER_OFFLINE"
	next.ErrorMessage = "No Worker claimed the job"
	return next, true
}

// ProcessingDeadline starts after claim, never at queue creation. A genuine
// failed worker attempt may permit the parent's next provider; busy/leased
// work never authorizes an overlapping HTTP fallback.
func ProcessingDeadline(j Job, timeoutSeconds int) (time.Time, bool) {
	if j.LeasedAt.IsZero() || timeoutSeconds < 1 || timeoutSeconds > 300 {
		return time.Time{}, false
	}
	return j.LeasedAt.Add(time.Duration(timeoutSeconds) * time.Second), true
}
func AllowsNextProvider(j Job) bool { return j.Status == Failed }

func terminalResponse(j Job, idempotent bool) TerminalResponse {
	v := TerminalResponse{JobID: j.JobID, Status: j.Status, Idempotent: idempotent}
	if j.Status == Failed {
		r := Retryable(0, j.ErrorCode)
		v.Retryable = &r
	}
	return v
}
func terminalReplay(j Job, expected Status, identity LeaseRequest, secret []byte, key, hash string) (TerminalResponse, bool, error) {
	if j.JobID == "" {
		return TerminalResponse{}, true, NewError("JOB_NOT_FOUND")
	}
	switch j.Status {
	case Cancelled:
		return TerminalResponse{}, true, NewError("JOB_CANCELLED")
	case Expired:
		return TerminalResponse{}, true, NewError("LEASE_EXPIRED")
	case Succeeded, Failed:
		if j.Status != expected {
			return TerminalResponse{}, true, NewError("JOB_TERMINAL_CONFLICT")
		}
		if err := verifyIdentity(j, identity, secret); err != nil {
			return TerminalResponse{}, true, err
		}
		// No legacy empty-hash bypass: the normative spec requires exact content.
		if !equalConstant(j.CompletionKey, key) || !equalConstant(j.CompletionHash, hash) {
			return TerminalResponse{}, true, NewError("IDEMPOTENCY_CONFLICT")
		}
		return terminalResponse(j, true), true, nil
	}
	return TerminalResponse{}, false, nil
}
func Complete(j Job, request CompleteRequest, signingSecret []byte, now time.Time) (Job, TerminalResponse, error) {
	hash, err := CompleteFingerprint(request)
	if err != nil {
		return j, TerminalResponse{}, err
	}
	identity := LeaseRequest{WorkerID: request.WorkerID, LeaseID: request.LeaseID}
	if response, done, err := terminalReplay(j, Succeeded, identity, signingSecret, request.IdempotencyKey, hash); done {
		return j, response, err
	}
	if err := VerifyLease(j, identity, signingSecret, now); err != nil {
		return j, TerminalResponse{}, err
	}
	result := map[string]any{}
	if j.Capability == Image {
		found := false
		for _, artifact := range j.Artifacts {
			if artifact.Response.ArtifactID == request.ArtifactID && artifact.JobID == j.JobID && artifact.WorkerID == j.WorkerID && equalConstant(artifact.LeaseHash, j.LeaseHash) {
				found = true
				v := artifact.Response
				result["artifact_id"] = v.ArtifactID
				result["content_type"] = v.ContentType
				result["width"] = v.Width
				result["height"] = v.Height
				result["format"] = v.Format
				break
			}
		}
		if !found {
			return j, TerminalResponse{}, NewError("ARTIFACT_REQUIRED")
		}
	} else if j.Capability == Text {
		if _, err := ExtractText(request.Result); err != nil {
			return j, TerminalResponse{}, NewError("RESULT_REQUIRED")
		}
		result["result"] = request.Result
	} else {
		return j, TerminalResponse{}, NewError("CAPABILITY_INVALID")
	}
	if len(request.Metadata) > 0 {
		result["metadata"] = request.Metadata
	}
	b, err := json.Marshal(result)
	if err != nil {
		return j, TerminalResponse{}, NewError("REQUEST_SCHEMA_INVALID")
	}
	if len(b) > MaxResultBytes {
		return j, TerminalResponse{}, NewError("RESULT_TOO_LARGE")
	}
	next := cloneJob(j)
	next.Status = Succeeded
	next.ResultJSON = b
	next.CompletionKey = request.IdempotencyKey
	next.CompletionHash = hash
	next.CompletedAt = now
	next.ErrorCode = ""
	next.ErrorMessage = ""
	return next, terminalResponse(next, false), nil
}
func Fail(j Job, request FailRequest, signingSecret []byte, now time.Time) (Job, TerminalResponse, error) {
	hash, err := FailFingerprint(request)
	if err != nil {
		return j, TerminalResponse{}, err
	}
	identity := LeaseRequest{WorkerID: request.WorkerID, LeaseID: request.LeaseID}
	if response, done, err := terminalReplay(j, Failed, identity, signingSecret, request.IdempotencyKey, hash); done {
		return j, response, err
	}
	if err := VerifyLease(j, identity, signingSecret, now); err != nil {
		return j, TerminalResponse{}, err
	}
	next := cloneJob(j)
	next.Status = Failed
	next.ErrorCode = NormalizeErrorCode(request.ErrorCode)
	next.ErrorMessage = SanitizeErrorMessage(request.ErrorMessage)
	next.CompletionKey = request.IdempotencyKey
	next.CompletionHash = hash
	next.CompletedAt = now
	return next, terminalResponse(next, false), nil
}

// RecordArtifact verifies exact uploaded bytes and normalized metadata. The
// parent must first decode/normalize the image, enforce MIME/magic/byte limits
// and calculate candidate.SHA256 from the actual stored bytes. This function
// neither decodes images nor writes files, including on duplicate replay.
func RecordArtifact(j Job, request ArtifactForm, source []byte, candidate ArtifactResponse, signingSecret []byte, now time.Time, maxBytes int64) (Job, ArtifactResponse, error) {
	if err := request.Validate(); err != nil {
		return j, ArtifactResponse{}, err
	}
	if j.JobID == "" {
		return j, ArtifactResponse{}, NewError("JOB_NOT_FOUND")
	}
	if j.Status == Cancelled {
		return j, ArtifactResponse{}, NewError("JOB_CANCELLED")
	}
	identity := LeaseRequest{WorkerID: request.WorkerID, LeaseID: request.LeaseID}
	for _, existing := range j.Artifacts {
		if existing.JobID == j.JobID && existing.IdempotencyKey == request.IdempotencyKey {
			if err := verifyIdentity(j, identity, signingSecret); err != nil {
				return j, ArtifactResponse{}, err
			}
			if existing.WorkerID != request.WorkerID {
				return j, ArtifactResponse{}, NewError("ARTIFACT_WORKER_MISMATCH")
			}
			if !equalConstant(existing.LeaseHash, j.LeaseHash) {
				return j, ArtifactResponse{}, NewError("LEASE_ID_INVALID")
			}
			if !equalConstant(strings.ToLower(existing.Response.SourceSHA256), strings.ToLower(request.SHA256)) || !equalConstant(strings.ToLower(existing.Response.SourceSHA256), SHA256(source)) {
				return j, ArtifactResponse{}, NewError("IDEMPOTENCY_CONFLICT")
			}
			v := existing.Response
			v.Idempotent = true
			return j, v, nil
		}
	}
	if err := VerifyLease(j, identity, signingSecret, now); err != nil {
		return j, ArtifactResponse{}, err
	}
	if j.Capability != Image {
		return j, ArtifactResponse{}, NewError("ARTIFACT_NOT_ALLOWED")
	}
	if err := VerifyArtifactSource(request.SHA256, source, maxBytes); err != nil {
		return j, ArtifactResponse{}, err
	}
	if err := candidate.Validate(); err != nil {
		return j, ArtifactResponse{}, err
	}
	if !strings.EqualFold(candidate.SourceSHA256, request.SHA256) {
		return j, ArtifactResponse{}, NewError("ARTIFACT_SHA256_MISMATCH")
	}
	for _, existing := range j.Artifacts {
		if existing.Response.ArtifactID == candidate.ArtifactID {
			return j, ArtifactResponse{}, NewError("IDEMPOTENCY_CONFLICT")
		}
	}
	candidate.Idempotent = false
	candidate.SHA256 = strings.ToLower(candidate.SHA256)
	candidate.SourceSHA256 = strings.ToLower(candidate.SourceSHA256)
	next := cloneJob(j)
	next.Artifacts = append(next.Artifacts, ArtifactRecord{JobID: j.JobID, WorkerID: j.WorkerID, LeaseHash: j.LeaseHash, IdempotencyKey: request.IdempotencyKey, Response: candidate})
	return next, candidate, nil
}
