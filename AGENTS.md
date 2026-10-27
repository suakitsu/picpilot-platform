# PicPilot Go development

- This repository is private until its owner explicitly authorizes publication.
- The Java project is a read-only reference. Never access production databases or secrets for tests.
- All automated provider tests must use local mock servers; never create paid model tasks.
- Keep user credentials, uploads, databases, backups and generated binaries outside Git.
- Preserve the Local Worker Protocol v1 wire format and its lease/idempotency guarantees.
- Never log authorization headers, keys, full prompts, image bytes or payment secrets.
- A successful build is not feature parity. Keep the migration acceptance checklist accurate.
- The shared Go module lives in `core`; the personal release includes a verified snapshot of it so cloning either private repository is sufficient to build.
