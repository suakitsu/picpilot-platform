// Package workerprotocol implements PicPilot Local Worker Protocol v1 wire
// validation and deterministic, copy-on-write lease/idempotency decisions.
//
// It does not authenticate tokens, generate secrets, access storage, start
// timers, make requests, or settle business tasks. Callers must authenticate,
// lock current state, apply business cancellation, validate ownership, and
// atomically persist returned transitions and receipts. See the integration
// notes in docs/migration for the complete boundary and acceptance gaps.
package workerprotocol
