package workerprotocol

import "time"

// Worker contains opaque token identity, not a raw credential. Authenticating
// token hash/revocation is mandatory in the parent on EVERY request/replay.
type Worker struct {
	WorkerID       string
	TokenID        string
	Capabilities   []Capability
	MaxConcurrency int
	LastHeartbeat  time.Time
}

func BindWorker(existing *Worker, tokenID string, request HeartbeatRequest, now time.Time, activeTasks, offlineSeconds int) (Worker, HeartbeatResponse, error) {
	if err := request.Validate(); err != nil {
		return Worker{}, HeartbeatResponse{}, err
	}
	if tokenID == "" {
		return Worker{}, HeartbeatResponse{}, NewError("WORKER_TOKEN_INVALID")
	}
	if existing != nil && (existing.WorkerID != request.WorkerID || existing.TokenID != tokenID) {
		return Worker{}, HeartbeatResponse{}, NewError("WORKER_ID_IN_USE")
	}
	if now.IsZero() || activeTasks < 0 || offlineSeconds < 15 || offlineSeconds > 300 {
		return Worker{}, HeartbeatResponse{}, NewError("REQUEST_SCHEMA_INVALID")
	}
	w := Worker{WorkerID: request.WorkerID, TokenID: tokenID, Capabilities: append([]Capability{}, request.Capabilities...), MaxConcurrency: request.MaxConcurrency, LastHeartbeat: now}
	r := HeartbeatResponse{WorkerID: request.WorkerID, ServerTime: now, ActiveTasks: activeTasks, HeartbeatIntervalSeconds: offlineSeconds / 3}
	return w, r, nil
}
func VerifyWorkerOwnership(worker *Worker, tokenID, workerID string) error {
	if err := ValidateWorkerID(workerID); err != nil {
		return err
	}
	if worker == nil {
		return NewError("WORKER_HEARTBEAT_REQUIRED")
	}
	if tokenID == "" {
		return NewError("WORKER_TOKEN_INVALID")
	}
	if worker.WorkerID != workerID || worker.TokenID != tokenID {
		return NewError("WORKER_TOKEN_MISMATCH")
	}
	return nil
}

// VerifyInputAccess does not take a raw lease ID because signed input GETs do
// not carry one in v1. Token ownership must already have been checked above.
func VerifyInputAccess(j Job, workerID string, index int, expires int64, signature, sourceHTTPSURL string, signingSecret []byte, now time.Time) error {
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
	if !IsActive(j, now) {
		return NewError("LEASE_EXPIRED")
	}
	if j.WorkerID != workerID {
		return NewError("WORKER_MISMATCH")
	}
	// Original server treats expires as an inclusive Unix-second boundary.
	if expires < now.Unix() {
		return NewError("INPUT_URL_EXPIRED")
	}
	if index < 0 || index >= len(j.InputFiles) {
		return NewError("INPUT_NOT_FOUND")
	}
	expected, err := InputSignature(signingSecret, j.JobID, index, workerID, expires, sourceHTTPSURL)
	if err != nil {
		return err
	}
	if !equalConstant(expected, signature) {
		return NewError("INPUT_SIGNATURE_INVALID")
	}
	return nil
}
