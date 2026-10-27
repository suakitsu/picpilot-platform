# Migration Parity Checklist

Status: all original Java mappings enumerated; each endpoint remains unimplemented or handler-present/parity-unverified as recorded below. Pure protocol logic is not end-to-end parity.

## Provenance

- Read-only reference: `E:/picpilot/workspace/openImage`.
- Controller hashes, source locations, request signatures, response evidence and frontend callers are recorded in `api-inventory.json`.
- 19 controllers; 154 annotated methods; 155 method/content-type variants; 150 unique method/path pairs.
- 142 browser API call sites; 17 unmatched static calls; 20 dynamic fetch sites require contextual review.
- Rebuild with `node docs/migration/build-inventory.cjs E:/picpilot/workspace/openImage`; reads source only and writes these two inventory files. Uses the original installed TypeScript parser without installing dependencies.
- Source-level shapes are not executable acceptance results. Entity/record and frontend declarations are retained alongside map-return evidence; optional/dynamic branches still need mock contract tests.

## Parent Delegation

- 47 original routes currently have Go handlers; presence is not a verified matching request/response/workflow contract. New Go setup/provider routes are listed separately.
- See `canvas-runtime-parity.md` and `canvas-runtime-inventory.json` for the high-priority Nova routes, bridge mapping, clean-source gaps, and full-scope backend work packages.

## Full Acceptance Scope

- [x] Two new GitHub repositories created PRIVATE, reported by parent/user on 2026-10-26. Source not pushed yet; no publication authorized.
- [ ] Independently cloneable platform and personal repositories, verified core snapshot/version synchronization, clean-clone commands and private pushes.
- [ ] Immutable edition policies. Personal has no platform billing/admin UI or API dependency; platform retains all account, credit, payment and administrator workflows.
- [ ] PostgreSQL migrations, configuration, transactional isolation, ownership authorization, JWT/password/email-code/OAuth flows, rate limits, audit and safe error contracts.
- [ ] Personal per-user encrypted image/text/video provider settings, credential isolation, validation and no plaintext credential response/logging.
- [ ] Image/text/video adapters and routing: HTTP and pull worker; mock gateways, priority, disabled-provider behavior, failure/fallback, submission ambiguity and no duplicate paid jobs.
- [ ] Durable queues: atomic FIFO claims, global provider and per-worker concurrency, multi-instance row locks, restart recovery, terminal CAS, cancellation, expired queue attempts and late callback rejection.
- [ ] Local Worker v1 HTTP/TLS/auth adapters, one-time administrator token issuance/regeneration/revocation, persisted hash-only leases, signed input redirect and cross-origin Authorization stripping.
- [ ] Worker artifacts: MIME/magic decode, normalized orientation/metadata, SHA-256 of source and stored bytes, byte limit, 50 MP limit, deduplicated ownership and storage cleanup.
- [ ] Storage: local/object storage, ownership of references, upload/HEIC normalization, signed reads, download safety, uncropped full-resolution images and deletion/retention.
- [ ] Complete project/listing copy/fact confirmation/image generation/listing-package/A+ and linked-A+ flows; history, deletion, retries and exact-once settlement.
- [ ] Commerce/domestic suites: catalogs, slots, templates, estimates, references, generation, retries and administrator catalog editing.
- [ ] Creative chat: messages, versions, estimates, ordered multiple references, generated artifacts and continuity; image translation and assistant workflows.
- [ ] Video: configuration/model variants, script preview, queue/submit/poll/webhook reconciliation, delete/cancel, safe retry, output storage and refunds.
- [ ] Platform billing: plans/channels/orders, Afdian/Ykmcn webhook validation, reconciliation, ledger, redeem codes, benefits, idempotent credits and refunds.
- [ ] Platform administration: dashboard/trends/costs, users, credit adjustments, password/status, risk keywords/events, prompts/configuration, worker status/tokens, audit/text-call/image/video logs and messages.
- [ ] Public files/avatars/benefit assets/video files and canvas worker input serving, correct cache/redirect/content headers, no sensitive path exposure.
- [ ] Nova workspace AND infinite canvas: rebuild from corresponding source, not only copied dist; preserve projects/state, nodes/edges, upload/reference order, text/image/video generation, task polling and export.
- [ ] Canvas bridge and runtime-only routes: postMessage origin/session checks, image.submit/image.status/image.generate/text.generate/video.generate; /api/nova/proxy/text, /api/nova/proxy/models, /api/nova/queue-status, /api/nova/tasks compatibility. These are not Java controller endpoints.
- [ ] Native Windows/Android callers and embedded canvas compatibility, token/session isolation, media downloads and history; native route references are inventoried, not ported.
- [ ] First-run wizard, upgrades, backups/restores, independently deployable platform/personal packages; README.md/README.en.md/README.ja.md commands verified.
- [ ] Source/license/history/secret review and mock full-flow desktop/mobile browser acceptance. No live paid model calls or production data for verification.

## Integration Boundary

Initial scope was `docs/migration` and `core/workerprotocol`. The authorized second stage adds `core/store/worker*.go`, migration `0002_worker.sql`, and `core/server/workers.go` with tests. See `workerprotocol-integration.md` and `worker-http-integration.md`. Parent still owns route mounting, business dispatch/settlement, general auth/storage/provider APIs and full original parity. Source handler presence alone does not close an endpoint gate.

## Endpoint Checklist

| Status | HTTP | Path | Handler | Response | Browser callers |
| --- | --- | --- | --- | --- | --- |
| [ ] unimplemented | GET | `/api/admin/ai-config` | AdminAiConfigController.getConfig | ApiResponse<Map<String, Object>> | getAiRuntimeConfig |
| [ ] unimplemented | PATCH | `/api/admin/ai-config` | AdminAiConfigController.updateConfig | ApiResponse<Map<String, Object>> | updateAiRuntimeConfig |
| [ ] unimplemented | POST | `/api/admin/ai-config/probe/{kind}` | AdminAiConfigController.probeConfig | ApiResponse<Map<String, Object>> | probeAiRuntimeConfig |
| [ ] unimplemented | POST | `/api/admin/ai-config/rollback` | AdminAiConfigController.rollbackConfig | ApiResponse<Map<String, Object>> | rollbackAiRuntimeConfig |
| [ ] unimplemented | POST | `/api/admin/ai-config/validate` | AdminAiConfigController.validateConfig | ApiResponse<Map<String, Object>> | validateAiRuntimeConfig |
| [ ] handler present; parity unverified | POST | `/api/admin/ai-config/worker-tokens` | AdminAiConfigController.createWorkerToken | ApiResponse<Map<String, Object>> | createAiWorkerToken |
| [ ] handler present; parity unverified | POST | `/api/admin/ai-config/worker-tokens/{tokenId}/regenerate` | AdminAiConfigController.regenerateWorkerToken | ApiResponse<Map<String, Object>> | regenerateAiWorkerToken |
| [ ] handler present; parity unverified | POST | `/api/admin/ai-config/worker-tokens/{tokenId}/revoke` | AdminAiConfigController.revokeWorkerToken | ApiResponse<Map<String, Object>> | revokeAiWorkerToken |
| [ ] handler present; parity unverified | GET | `/api/admin/ai-config/workers` | AdminAiConfigController.workerState | ApiResponse<Map<String, Object>> | getAiWorkerState |
| [ ] unimplemented | GET | `/api/admin/audit-logs` | AdminController.auditLogs | ApiResponse<Map<String, Object>> | listAuditLogs |
| [ ] unimplemented | POST | `/api/admin/benefits/card-waves` | AdminController.createBenefitCardWave | ApiResponse<Map<String, Object>> | createBenefitCardWave |
| [ ] unimplemented | GET | `/api/admin/benefits/config` | AdminController.benefitConfig | ApiResponse<Map<String, Object>> | getBenefitConfig |
| [ ] unimplemented | POST | `/api/admin/benefits/config` | AdminController.updateBenefitConfig | ApiResponse<Map<String, Object>> | updateBenefitConfig |
| [ ] unimplemented | POST | `/api/admin/benefits/qr` | AdminController.uploadBenefitQr | ApiResponse<Map<String, Object>> | uploadBenefitQr |
| [ ] unimplemented | GET | `/api/admin/benefits/redemption-codes` | AdminController.benefitRedemptionCodes | ApiResponse<Map<String, Object>> | listBenefitRedemptionCodes |
| [ ] unimplemented | GET | `/api/admin/benefits/redemption-records` | AdminController.redemptionRecords | ApiResponse<Map<String, Object>> | listRedemptionRecords |
| [ ] unimplemented | GET | `/api/admin/billing/channels` | AdminController.paymentChannels | ApiResponse<List<PaymentChannelEntity>> | listPaymentChannels |
| [ ] unimplemented | POST | `/api/admin/billing/channels/{id}` | AdminController.updatePaymentChannel | ApiResponse<PaymentChannelEntity> | updatePaymentChannel |
| [ ] unimplemented | GET | `/api/admin/billing/orders` | AdminController.rechargeOrders | ApiResponse<Map<String, Object>> | listRechargeOrders |
| [ ] unimplemented | PATCH | `/api/admin/billing/orders/{id}/cancel` | AdminController.cancelRechargeOrder | ApiResponse<Map<String, Object>> | cancelRechargeOrder |
| [ ] unimplemented | PATCH | `/api/admin/billing/orders/{id}/confirm` | AdminController.confirmRechargeOrder | ApiResponse<Map<String, Object>> | confirmRechargeOrder |
| [ ] unimplemented | GET | `/api/admin/billing/plans` | AdminController.rechargePlans | ApiResponse<List<RechargePlanEntity>> | listRechargePlans |
| [ ] unimplemented | POST | `/api/admin/billing/plans/{id}` | AdminController.updateRechargePlan | ApiResponse<RechargePlanEntity> | updateRechargePlan |
| [ ] unimplemented | GET | `/api/admin/billing/redemption-codes` | AdminController.redemptionCodes | ApiResponse<Map<String, Object>> | listRedemptionCodes |
| [ ] unimplemented | POST | `/api/admin/billing/redemption-codes` | AdminController.createRedemptionCodes | ApiResponse<List<RedemptionCodeEntity>> | createRedemptionCodes |
| [ ] unimplemented | GET | `/api/admin/commerce-suite/catalog` | AdminCommerceSuiteController.catalog | ApiResponse<CommerceSuiteDtos.Catalog> | getAdminCommerceSuiteCatalog |
| [ ] unimplemented | POST | `/api/admin/commerce-suite/catalog` | AdminCommerceSuiteController.saveCatalog | ApiResponse<CommerceSuiteDtos.Catalog> | saveAdminCommerceSuiteCatalog |
| [ ] unimplemented | GET | `/api/admin/commerce-suite/platforms` | AdminCommerceSuiteController.platforms | ApiResponse<List<CommerceSuiteDtos.PlatformPreset>> | listAdminCommerceSuitePlatforms |
| [ ] unimplemented | PUT | `/api/admin/commerce-suite/platforms/{code}` | AdminCommerceSuiteController.savePlatform | ApiResponse<CommerceSuiteDtos.PlatformPreset> | none found; see inventory |
| [ ] unimplemented | GET | `/api/admin/commerce-suite/templates` | AdminCommerceSuiteController.templates | ApiResponse<List<CommerceSuiteDtos.TemplatePreset>> | listAdminCommerceSuiteTemplates |
| [ ] unimplemented | PUT | `/api/admin/commerce-suite/templates/{id}` | AdminCommerceSuiteController.saveTemplate | ApiResponse<CommerceSuiteDtos.TemplatePreset> | none found; see inventory |
| [ ] unimplemented | GET | `/api/admin/costs` | AdminController.costs | ApiResponse<Map<String, Object>> | getCostSummary |
| [ ] unimplemented | GET | `/api/admin/dashboard` | AdminController.dashboard | ApiResponse<Map<String, Object>> | getDashboard |
| [ ] unimplemented | GET | `/api/admin/dashboard/trends` | AdminController.dashboardTrends | ApiResponse<List<Map<String, Object>>> | none found; see inventory |
| [ ] unimplemented | GET | `/api/admin/image-configs` | AdminController.imageConfigs | ApiResponse<List<ImageTypeConfigEntity>> | listImageConfigs |
| [ ] unimplemented | POST | `/api/admin/image-configs/{id}` | AdminController.updateImageConfig | ApiResponse<ImageTypeConfigEntity> | updateImageConfig |
| [ ] unimplemented | GET | `/api/admin/image-tasks` | AdminController.tasks | ApiResponse<Map<String, Object>> | listImageTasks |
| [ ] unimplemented | GET | `/api/admin/image-tasks/{id}` | AdminController.imageTaskDetail | ApiResponse<Map<String, Object>> | getImageTask |
| [ ] unimplemented | PATCH | `/api/admin/image-tasks/{id}/delete` | AdminController.deleteTask | ApiResponse<Map<String, Object>> | deleteImageTask |
| [ ] unimplemented | PATCH | `/api/admin/image-tasks/{id}/retry` | AdminController.retryTask | ApiResponse<Map<String, Object>> | retryImageTask |
| [ ] unimplemented | GET | `/api/admin/messages` | AdminController.stationMessages | ApiResponse<Map<String, Object>> | listStationMessages |
| [ ] unimplemented | POST | `/api/admin/messages` | AdminController.publishStationMessage | ApiResponse<StationMessageEntity> | publishStationMessage |
| [ ] unimplemented | GET | `/api/admin/prompts` | AdminController.prompts | ApiResponse<List<PromptTemplateEntity>> | listPromptTemplates |
| [ ] unimplemented | POST | `/api/admin/prompts` | AdminController.createPrompt | ApiResponse<PromptTemplateEntity> | createPromptTemplate |
| [ ] unimplemented | POST | `/api/admin/prompts/{id}` | AdminController.updatePrompt | ApiResponse<PromptTemplateEntity> | updatePromptTemplate |
| [ ] unimplemented | PATCH | `/api/admin/risk-config` | AdminController.updateRiskConfig | ApiResponse<Map<String, Object>> | updateRiskConfig |
| [ ] unimplemented | GET | `/api/admin/risk-events` | AdminController.riskEvents | ApiResponse<Map<String, Object>> | listRiskEvents |
| [ ] unimplemented | GET | `/api/admin/risk-keywords` | AdminController.riskKeywords | ApiResponse<Map<String, Object>> | getRiskKeywords |
| [ ] unimplemented | POST | `/api/admin/risk-keywords` | AdminController.addRiskKeyword | ApiResponse<Map<String, Object>> | addRiskKeyword |
| [ ] unimplemented | POST | `/api/admin/risk-keywords/delete` | AdminController.deleteRiskKeyword | ApiResponse<Map<String, Object>> | deleteRiskKeyword |
| [ ] unimplemented | GET | `/api/admin/text-calls` | AdminTextCallController.list | ApiResponse<Map<String, Object>> | listTextCalls |
| [ ] unimplemented | GET | `/api/admin/users` | AdminController.users | ApiResponse<Map<String, Object>> | listAdminUsers |
| [ ] unimplemented | POST | `/api/admin/users/{id}/credits` | AdminController.adjustCredits | ApiResponse<Map<String, Object>> | adjustUserCredits |
| [ ] unimplemented | POST | `/api/admin/users/{id}/password` | AdminController.resetUserPassword | ApiResponse<Map<String, Object>> | resetUserPassword |
| [ ] unimplemented | POST | `/api/admin/users/{id}/risk-events/clear` | AdminController.clearUserRiskEvents | ApiResponse<Map<String, Object>> | clearUserRiskEvents |
| [ ] unimplemented | PATCH | `/api/admin/users/{id}/status` | AdminController.updateUserStatus | ApiResponse<Map<String, Object>> | updateUserStatus |
| [ ] handler present; parity unverified | GET | `/api/admin/video-config` | VideoAdminController.get | ApiResponse<Map<String, Object>> | getVideoAdminConfig |
| [ ] handler present; parity unverified | PATCH | `/api/admin/video-config` | VideoAdminController.update | ApiResponse<Map<String, Object>> | updateVideoAdminConfig |
| [ ] handler present; parity unverified | GET | `/api/admin/video-tasks` | VideoTaskAdminController.list | ApiResponse<Map<String, Object>> | listAdminVideoTasks |
| [ ] handler present; parity unverified | DELETE | `/api/admin/video-tasks/{id}` | VideoTaskAdminController.delete | ApiResponse<Map<String, Object>> | deleteAdminVideoTask |
| [ ] handler present; parity unverified | POST | `/api/assistant/chat` | AssistantController.chat | ApiResponse<AssistantDtos.AssistantChatResponse> | chatWithAssistant |
| [ ] handler present; parity unverified | GET | `/api/assistant/status` | AssistantController.status | ApiResponse<AssistantDtos.AssistantStatusResponse> | getAssistantStatus |
| [ ] handler present; parity unverified | POST | `/api/auth/admin/login` | AuthController.adminLogin | ApiResponse<AuthDtos.AuthResponse> | adminLogin |
| [ ] handler present; parity unverified | POST | `/api/auth/admin/register` | AuthController.adminRegister | ApiResponse<AuthDtos.AuthResponse> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/auth/email-code` | AuthController.sendEmailCode | ApiResponse<java.util.Map<String, Object>> | sendEmailCode |
| [ ] handler present; parity unverified | POST | `/api/auth/forgot-password` | AuthController.forgotPassword | ApiResponse<java.util.Map<String, Object>> | requestPasswordReset |
| [ ] handler present; parity unverified | GET | `/api/auth/github` | AuthController.githubLogin | ResponseEntity<Void> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/auth/github/callback` | AuthController.githubCallback | ResponseEntity<Void> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/auth/github/status` | AuthController.githubStatus | ApiResponse<java.util.Map<String, Object>> | githubStatus |
| [ ] handler present; parity unverified | POST | `/api/auth/login` | AuthController.login | ApiResponse<AuthDtos.AuthResponse> | login |
| [ ] handler present; parity unverified | POST | `/api/auth/login/email-code` | AuthController.loginWithEmailCode | ApiResponse<AuthDtos.AuthResponse> | loginWithEmailCode |
| [ ] handler present; parity unverified | POST | `/api/auth/password-reset` | AuthController.resetPassword | ApiResponse<java.util.Map<String, Object>> | confirmPasswordReset |
| [ ] handler present; parity unverified | POST | `/api/auth/register` | AuthController.register | ApiResponse<AuthDtos.AuthResponse> | register |
| [ ] handler present; parity unverified | POST | `/api/billing/afdian/webhook` | BillingController.afdianWebhook | Map<String, Object> | none found; see inventory |
| [ ] unimplemented | GET | `/api/billing/channels` | BillingController.channels | ApiResponse<List<Map<String, Object>>> | listPaymentChannels |
| [ ] unimplemented | GET | `/api/billing/ledger` | BillingController.ledger | ApiResponse<Map<String, Object>> | listCreditLedger |
| [ ] unimplemented | GET | `/api/billing/orders` | BillingController.orders | ApiResponse<Map<String, Object>> | listRechargeOrders |
| [ ] unimplemented | POST | `/api/billing/orders` | BillingController.createOrder | ApiResponse<Map<String, Object>> | createRechargeOrder |
| [ ] unimplemented | POST | `/api/billing/orders/{id}/cancel` | BillingController.cancelOrder | ApiResponse<Map<String, Object>> | cancelRechargeOrder |
| [ ] unimplemented | POST | `/api/billing/orders/{id}/sync` | BillingController.syncOrder | ApiResponse<Map<String, Object>> | syncRechargeOrder |
| [ ] unimplemented | GET | `/api/billing/plans` | BillingController.plans | ApiResponse<List<RechargePlanEntity>> | listRechargePlans |
| [ ] unimplemented | POST | `/api/billing/redeem` | BillingController.redeem | ApiResponse<Map<String, Object>> | redeemCode |
| [ ] unimplemented | GET | `/api/billing/summary` | BillingController.summary | ApiResponse<Map<String, Object>> | getBillingSummary |
| [ ] handler present; parity unverified | GET | `/api/billing/ykmcn/notify` | BillingController.ykmcnNotify | String | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/billing/ykmcn/notify` | BillingController.ykmcnNotify | String | none found; see inventory |
| [ ] unimplemented | GET | `/api/commerce-suite/catalog` | CommerceSuiteController.catalog | ApiResponse<CommerceSuiteDtos.Catalog> | getCommerceSuiteCatalog |
| [ ] unimplemented | POST | `/api/commerce-suite/estimate` | CommerceSuiteController.estimate | ApiResponse<CommerceSuiteDtos.EstimateResponse> | estimateCommerceSuite |
| [ ] unimplemented | POST | `/api/commerce-suite/generate` (multipart/form-data) | CommerceSuiteController.generateForm | ApiResponse<CommerceSuiteDtos.GenerateResponse> | generateCommerceSuite |
| [ ] unimplemented | POST | `/api/commerce-suite/generate` (application/json) | CommerceSuiteController.generateJson | ApiResponse<CommerceSuiteDtos.GenerateResponse> | generateCommerceSuite |
| [ ] unimplemented | GET | `/api/commerce-suite/platforms` | CommerceSuiteController.platforms | ApiResponse<List<CommerceSuiteDtos.PlatformPreset>> | listCommerceSuitePlatforms |
| [ ] unimplemented | POST | `/api/commerce-suite/projects` | CommerceSuiteController.createProject | ApiResponse<CommerceSuiteDtos.GenerateResponse> | none found; see inventory |
| [ ] unimplemented | GET | `/api/commerce-suite/projects/{projectId}` | CommerceSuiteController.project | ApiResponse<ProjectDtos.ProjectDetail> | none found; see inventory |
| [ ] unimplemented | GET | `/api/commerce-suite/tasks/{taskId}` | CommerceSuiteController.task | ApiResponse<ProjectDtos.ImageTaskView> | none found; see inventory |
| [ ] unimplemented | POST | `/api/commerce-suite/tasks/{taskId}/retry` | CommerceSuiteController.retry | ApiResponse<ProjectDtos.ImageTaskView> | retryCommerceSuiteTask |
| [ ] unimplemented | GET | `/api/commerce-suite/templates` | CommerceSuiteController.templates | ApiResponse<List<CommerceSuiteDtos.TemplatePreset>> | listCommerceSuiteTemplates |
| [ ] unimplemented | POST | `/api/image-translation/generate` (multipart/form-data) | ImageTranslationController.generate | ApiResponse<ImageTranslationDtos.GenerateResponse> | generateImageTranslation |
| [ ] unimplemented | GET | `/api/image-translation/languages` | ImageTranslationController.languages | ApiResponse<List<ImageTranslationDtos.LanguageOption>> | listImageTranslationLanguages |
| [ ] unimplemented | GET | `/api/image-translation/projects/{projectId}` | ImageTranslationController.project | ApiResponse<ProjectDtos.ProjectDetail> | none found; see inventory |
| [ ] unimplemented | GET | `/api/image-translation/tasks/{taskId}` | ImageTranslationController.task | ApiResponse<ProjectDtos.ImageTaskView> | none found; see inventory |
| [ ] unimplemented | POST | `/api/image-translation/tasks/{taskId}/retry` | ImageTranslationController.retry | ApiResponse<ProjectDtos.ImageTaskView> | retryImageTranslationTask |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/claim` | AiWorkerController.claim | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/heartbeat` | AiWorkerController.heartbeat | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/internal/picpilot-worker/v1/inputs/{jobId}/{inputIndex}` | AiWorkerController.input | ResponseEntity<Void> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/jobs/{jobId}/artifacts` (multipart/form-data) | AiWorkerController.uploadArtifact | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/jobs/{jobId}/complete` | AiWorkerController.complete | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/jobs/{jobId}/fail` | AiWorkerController.fail | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/internal/picpilot-worker/v1/jobs/{jobId}/renew` | AiWorkerController.renew | Map<String, Object> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/me` | AuthController.me | ApiResponse<AuthDtos.UserProfile> | getMe |
| [ ] handler present; parity unverified | PATCH | `/api/me` | AuthController.updateMe | ApiResponse<AuthDtos.UserProfile> | updateMe |
| [ ] handler present; parity unverified | POST | `/api/me/avatar` | AuthController.uploadAvatar | ApiResponse<AuthDtos.UserProfile> | uploadAvatar |
| [ ] handler present; parity unverified | GET | `/api/messages` | MessageController.list | ApiResponse<List<Map<String, Object>>> | listMessages |
| [ ] handler present; parity unverified | PATCH | `/api/messages/{id}/read` | MessageController.markRead | ApiResponse<Map<String, Object>> | markMessageRead |
| [ ] unimplemented | GET | `/api/projects` | ProjectController.list | ApiResponse<List<ProjectDtos.ProjectSummary>> | listProjects |
| [ ] unimplemented | POST | `/api/projects` | ProjectController.create | ApiResponse<ProjectDtos.ProjectSummary> | createProject |
| [ ] unimplemented | DELETE | `/api/projects/{id}` | ProjectController.delete | ApiResponse<java.util.Map<String, Object>> | deleteProject |
| [ ] unimplemented | GET | `/api/projects/{id}` | ProjectController.detail | ApiResponse<ProjectDtos.ProjectDetail> | getProject |
| [ ] unimplemented | POST | `/api/projects/{id}/confirm-facts` | ProjectController.facts | ApiResponse<ProjectDtos.ProjectSummary> | confirmFacts |
| [ ] unimplemented | POST | `/api/projects/{id}/generate-copy` | ProjectController.copy | ApiResponse<ProjectDtos.GenerateCopyResponse> | generateCopy |
| [ ] unimplemented | GET | `/api/projects/{id}/images` | ProjectController.listImages | ApiResponse<List<ProjectDtos.ImageTaskView>> | listProjectImages |
| [ ] unimplemented | POST | `/api/projects/{id}/images` | ProjectController.images | ApiResponse<List<ProjectDtos.ImageTaskView>> | enqueueImages |
| [ ] unimplemented | DELETE | `/api/projects/{id}/images/{taskId}` | ProjectController.deleteFailedImage | ApiResponse<java.util.Map<String, Object>> | deleteProjectImage |
| [ ] unimplemented | POST | `/api/projects/{id}/images/{taskId}/retry` (application/json) | ProjectController.retryImage | ApiResponse<ProjectDtos.ImageTaskView> | retryProjectImage |
| [ ] unimplemented | POST | `/api/projects/{id}/images/{taskId}/retry` (multipart/form-data) | ProjectController.retryImageForm | ApiResponse<ProjectDtos.ImageTaskView> | retryProjectImage |
| [ ] unimplemented | POST | `/api/projects/{id}/linked-aplus` | ProjectController.linkedAplusImage | ApiResponse<ProjectDtos.ProjectDetail> | createLinkedAplusImage |
| [ ] unimplemented | POST | `/api/projects/{id}/listing-package` | ProjectController.listingPackage | ApiResponse<ProjectDtos.ListingPackageResponse> | enqueueListingPackage |
| [ ] unimplemented | GET | `/api/projects/{id}/product-images` | ProjectController.productImages | ApiResponse<List<ProjectDtos.ProductImageView>> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/projects/{id}/product-images` | ProjectController.uploadProductImage | ApiResponse<ProjectDtos.ProductImageView> | uploadProductImage |
| [ ] unimplemented | POST | `/api/projects/{id}/product-images/link` | ProjectController.linkProductImage | ApiResponse<ProjectDtos.ProductImageView> | linkProductImage |
| [ ] unimplemented | POST | `/api/projects/aplus-images` (application/json) | ProjectController.aplusImage | ApiResponse<ProjectDtos.ProjectDetail> | createAplusImage |
| [ ] unimplemented | POST | `/api/projects/aplus-images` (multipart/form-data) | ProjectController.aplusImageForm | ApiResponse<ProjectDtos.ProjectDetail> | createAplusImage |
| [ ] unimplemented | POST | `/api/projects/canvas-text` | ProjectController.canvasText | ApiResponse<java.util.Map<String, Object>> | generateCanvasText |
| [ ] unimplemented | GET | `/api/projects/creative-chats` | ProjectController.creativeChats | ApiResponse<List<ProjectDtos.CreativeChatSummary>> | listCreativeChats |
| [ ] unimplemented | GET | `/api/projects/creative-chats/{chatId}` | ProjectController.creativeChat | ApiResponse<ProjectDtos.CreativeChatView> | getCreativeChat |
| [ ] unimplemented | POST | `/api/projects/creative-chats/estimate` | ProjectController.creativeChatEstimate | ApiResponse<ProjectDtos.CreativeChatEstimateResponse> | estimateCreativeChat |
| [ ] unimplemented | POST | `/api/projects/creative-chats/generate` (application/json) | ProjectController.creativeChatGenerate | ApiResponse<ProjectDtos.CreativeChatGenerateResponse> | generateCreativeChat |
| [ ] unimplemented | POST | `/api/projects/creative-chats/generate` (multipart/form-data) | ProjectController.creativeChatGenerateForm | ApiResponse<ProjectDtos.CreativeChatGenerateResponse> | generateCreativeChat |
| [ ] unimplemented | POST | `/api/projects/creative-images` (application/json) | ProjectController.creativeImage | ApiResponse<ProjectDtos.ProjectDetail> | createCreativeImage |
| [ ] unimplemented | POST | `/api/projects/creative-images` (multipart/form-data) | ProjectController.creativeImageForm | ApiResponse<ProjectDtos.ProjectDetail> | createCreativeImage |
| [ ] unimplemented | POST | `/api/projects/domestic-selling-points/suggest` | ProjectController.suggestDomesticSellingPoints | ApiResponse<java.util.Map<String, Object>> | suggestDomesticSellingPoints |
| [ ] unimplemented | POST | `/api/projects/optimize-prompt` | ProjectController.optimizePrompt | ApiResponse<java.util.Map<String, Object>> | optimizePrompt |
| [ ] handler present; parity unverified | GET | `/api/public/benefit-config` | PublicController.benefitConfig | ApiResponse<Map<String, Object>> | getBenefitConfig |
| [ ] unimplemented | GET | `/api/public/canvas-worker-inputs/{scope}/{filename}` | CanvasWorkerInputController.input | ResponseEntity<Resource> | none found; see inventory |
| [ ] unimplemented | GET | `/api/public/files/avatars/{userId}/{filename}` | PublicFileController.avatarFile | ResponseEntity<Resource> | none found; see inventory |
| [ ] unimplemented | GET | `/api/public/files/benefits/{kind}/{filename}` | PublicFileController.benefitFile | ResponseEntity<Resource> | none found; see inventory |
| [ ] unimplemented | GET | `/api/public/files/projects/{projectId}/{filename}` | PublicFileController.projectFile | ResponseEntity<Resource> | none found; see inventory |
| [ ] unimplemented | GET | `/api/public/files/videos/{filename}` | PublicFileController.videoFile | ResponseEntity<Resource> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/public/ping` | PublicController.ping | ApiResponse<Map<String, String>> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/public/video-webhooks/apimart` | VideoWebhookController.apimart | ApiResponse<Map<String, Object>> | none found; see inventory |
| [ ] handler present; parity unverified | POST | `/api/public/video-webhooks/apimart/{secret}/callback` | VideoWebhookController.apimartCallback | ApiResponse<Map<String, Object>> | none found; see inventory |
| [ ] handler present; parity unverified | GET | `/api/videos/config` | VideoController.config | ApiResponse<VideoDtos.PublicConfig> | getVideoConfig |
| [ ] handler present; parity unverified | POST | `/api/videos/script-preview` | VideoController.previewScript | ApiResponse<VideoDtos.VideoScriptPreviewResponse> | previewVideoScript |
| [ ] handler present; parity unverified | GET | `/api/videos/tasks` | VideoController.tasks | ApiResponse<List<VideoDtos.VideoTaskView>> | listVideoTasks |
| [ ] handler present; parity unverified | POST | `/api/videos/tasks` (multipart/form-data) | VideoController.create | ApiResponse<VideoDtos.VideoTaskView> | createVideoTask |
| [ ] handler present; parity unverified | DELETE | `/api/videos/tasks/{id}` | VideoController.delete | ApiResponse<Map<String, Object>> | deleteVideoTask |
| [ ] handler present; parity unverified | GET | `/api/videos/tasks/{id}` | VideoController.task | ApiResponse<VideoDtos.VideoTaskView> | getVideoTask |

## Known Evidence Gaps

- Dynamic fetches and runtime-generated asset URLs are recorded separately; no-caller does not mean unused. Payment callbacks, OAuth redirects, worker routes and storage routes have non-browser consumers.
- Static response keys are evidence, not a generated OpenAPI schema. Controller return expressions, referenced DTO/entity fields and exact frontend interface declarations must be checked against each runtime branch.
- Personal-specific provider-settings and bootstrap APIs are new requirements and are not implied to exist in the original Java route list.
- Canvas runtime APIs are implemented/intercepted outside Java; migration must retain both runtime behavior and Go business API compatibility.
- Pure transition tests cannot establish database atomicity, provider concurrency, token revocation, image decoding or one-time billing. Parent-owned isolated integration tests remain acceptance gates.
