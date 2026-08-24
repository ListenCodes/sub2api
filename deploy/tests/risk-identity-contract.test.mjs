import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const root = resolve(import.meta.dirname, '../..')
const read = (path) => readFileSync(resolve(root, path), 'utf8')
const identityImplementationPresent = existsSync(resolve(root, 'extensions-self/risk-control/identity_db.go'))

if (!identityImplementationPresent) {
	test('extension migration failure restores the extension without restarting main', () => {
		const apply = read('deploy/ops/apply-release.sh')
		assert.match(apply, /main_switch_started=false/)
		assert.match(apply, /restore_base_before_main_switch/)
		assert.match(apply, /if \[\[ "\$main_switch_started" == true \]\]; then/)
		assert.match(apply, /release_running_container_matches_image sub2api/)
		assert.match(apply, /main_switch_started=true\s+SUB2API_IMAGE=/)
	})
}

test('identity V2 remains additive and Shadow-backed with one explicit composite registration enforcement path', { skip: !identityImplementationPresent }, () => {
  const schema = read('extensions-self/risk-control/schema.sql')
  const main = read('extensions-self/risk-control/main.go')
  for (const table of ['risk_network_identities', 'risk_device_identities', 'risk_identity_events', 'risk_user_ip_links', 'risk_user_device_links', 'risk_identity_activity_daily', 'risk_identity_api_dedup', 'risk_identity_signals', 'risk_identity_signal_history', 'risk_identity_rebuild_jobs']) assert.match(schema, new RegExp(`CREATE TABLE IF NOT EXISTS ${table}`))
  assert.match(schema, /observing BOOLEAN NOT NULL DEFAULT TRUE CHECK \(observing = TRUE\)/)
  assert.match(schema, /mode VARCHAR\(16\) NOT NULL DEFAULT 'shadow' CHECK \(mode = 'shadow'\)/)
  assert.match(schema, /'v2_registration_ip_accounts', 'ip', TRUE/)
  assert.match(schema, /'v2_registration_device_accounts', 'device', TRUE/)
  assert.match(schema, /'v2_registration_composite_accounts', 'composite', TRUE/)
  assert.match(schema, /'v2_registration_email_retries', 'account', TRUE, 600, 5, 0, 'shadow'/)
  assert.doesNotMatch(schema, /DROP TABLE|DROP COLUMN/)
  assert.match(schema, /pg_get_constraintdef\(oid\) NOT LIKE '%account%'/)
  const repository = read('extensions-self/risk-control/identity_db.go')
	const identityService = read('extensions-self/risk-control/identity_service.go')
  assert.match(repository, /INSERT INTO risk_identity_signal_history[\s\S]*WHERE domain IN \('ip','device','composite'\)[\s\S]*DELETE FROM risk_identity_signals WHERE domain IN \('ip','device','composite'\)/)
  assert.match(repository, /rule\.subjectKind != "api_client" && event\.EventClass != "registration"/)
  assert.match(repository, /event\.EventType != "registration_attempt" \|\| event\.EmailLookupKey == ""[\s\S]*email_lookup_key=\$1/)
  assert.match(repository, /INSERT INTO risk_identity_signals\(event_id,user_id,domain,rule_code,rule_version_id,rule_revision,signal_family,score[\s\S]*VALUES \(\$1,0,'account'/)
  assert.match(main, /if cfg\.Identity\.RulesEnabled \{[\s\S]*EnsureShadowActivation[\s\S]*ActivateShadowRules/)
	assert.doesNotMatch(schema, /WHERE code IN \('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation'\)/)
	assert.match(schema, /AND code NOT IN \('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation'\)/)
	assert.match(repository, /DELETE FROM risk_rules WHERE code IN \('registration_abuse','registration_identity_abuse','registration_ip_multi_account','api_request_observation'\)/)
	assert.match(identityService, /RegistrationDecision[\s\S]*CompositeEnforcementEnabled[\s\S]*reject_candidate/)
	assert.match(repository, /v2_registration_composite_accounts[\s\S]*COUNT\(DISTINCT event\.user_id\)[\s\S]*identity_reject_candidate/)
	assert.match(repository, /home','company','school','trusted_egress','mobile_cgnat/)
	assert.doesNotMatch(identityService, /Action:\s*"(?:ban|auto_ban)"/)
})

test('compose passes independent identity secrets without weak defaults', { skip: !identityImplementationPresent }, () => {
  const compose = `${read('deploy/docker-compose.custom.yml')}\n${read('deploy/docker-compose.custom.local.yml')}`
  for (const key of ['RISK_IDENTITY_V2_ENABLED', 'RISK_IDENTITY_IP_COLLECTION_ENABLED', 'RISK_IDENTITY_DEVICE_COLLECTION_ENABLED', 'RISK_IDENTITY_ADMIN_ENABLED', 'RISK_IDENTITY_RULES_ENABLED', 'RISK_IDENTITY_IP_RULES_ENABLED', 'RISK_IDENTITY_DEVICE_RULES_ENABLED', 'RISK_IDENTITY_COMPOSITE_RULES_ENABLED', 'RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED', 'RISK_IDENTITY_CURRENT_SCORE_ENABLED', 'RISK_IDENTITY_CASES_ENABLED', 'RISK_IDENTITY_EXPLAIN_ENABLED', 'RISK_IDENTITY_DELIVERY_ENABLED']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-false\\}`))
  for (const key of ['RISK_IDENTITY_HMAC_KEY', 'RISK_IDENTITY_ENCRYPTION_KEY', 'RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY', 'RISK_DEVICE_COOKIE_SIGNING_KEY', 'RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-\\}`))
  for (const key of ['RISK_IDENTITY_ENCRYPTION_KEY_ID', 'RISK_IDENTITY_PREVIOUS_ENCRYPTION_KEY_ID', 'RISK_DEVICE_COOKIE_SIGNING_KEY_ID', 'RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY_ID']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-\\}`))
  assert.doesNotMatch(compose, /RISK_IDENTITY_(?:HMAC|ENCRYPTION)_KEY:.*change_this/)
})

test('identity rollout is a fixed-order ledger-backed configuration release', { skip: !identityImplementationPresent }, () => {
  const prepare = read('deploy/ops/prepare-identity-rollout.sh')
  const apply = read('deploy/ops/apply-release.sh')
  const common = read('deploy/ops/release-common.sh')
  const ledger = read('deploy/ops/release-ledger.sh')
  const dispatcher = read('deploy/ops/sync-and-publish.sh')
  for (const transition of ['stage0-safe-reset', 'stage1-v2', 'stage1-ip', 'stage1-device', 'stage2-admin', 'stage3-shadow-window', 'stage3-rules', 'stage4-geo', 'stage5-composite-enforcement']) {
    assert.match(prepare, new RegExp(transition))
    assert.match(common, new RegExp(transition))
  }
  assert.match(dispatcher, /update_kind \/\/ empty[^]*identity-config[^]*PREPARE_IDENTITY_SCRIPT/)
  assert.match(prepare, /release_create_complete_backup/)
  assert.match(prepare, /SUB2API_IDENTITY_SECRET_FILE:-\/etc\/sub2api\/identity-secrets\.env/)
  assert.match(prepare, /chmod 0600/)
  assert.match(prepare, /stat -c '%u' "\$secret_dir"/)
  assert.match(prepare, /stat -c '%a' "\$secret_dir"\)" == 700/)
  assert.match(prepare, /\$hmac_key" != "\$encryption_key" && "\$hmac_key" != "\$cookie_key" && "\$encryption_key" != "\$cookie_key/)
  assert.doesNotMatch(prepare, /release_job_update[^\n]*(HMAC_KEY|ENCRYPTION_KEY|COOKIE_SIGNING_KEY)/)
  assert.match(prepare, /date -u -d '\+15 days'/)
  assert.match(prepare, /date -u -d '\+14 days'/)
  assert.match(prepare, /docker network inspect deploy_sub2api-network/)
  assert.match(prepare, /SERVER_TRUSTED_PROXIES/)
  assert.match(prepare, /173\.245\.48\.0\/20/)
  assert.match(prepare, /2c0f:f248::\/32/)
  assert.match(read('deploy/docker-compose.custom.yml'), /SERVER_TRUSTED_PROXIES: \$\{SERVER_TRUSTED_PROXIES:-\}/)
  assert.match(apply, /validate_identity_runtime/)
  assert.match(prepare, /stage0-safe-reset[\s\S]*RISK_IDENTITY_V2_ENABLED[\s\S]*RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS[\s\S]*RISK_IDENTITY_SHADOW_UNTIL ''/)
  assert.match(apply, /if \[\[ "\$IDENTITY_TRANSITION" == stage0-safe-reset \]\]; then[\s\S]*identity\.domains\.ip == "disabled"[\s\S]*identity\.domains\.device == "disabled"[\s\S]*identity\.domains\.composite == "disabled"[\s\S]*identity\.effective_rule_count == 0[\s\S]*identity\.features\.delivery \| not/)
  assert.match(apply, /UPDATE_KIND[^\n]*identity-config[^\n]*IDENTITY_TRANSITION[^\n]*stage1-/)
  assert.match(apply, /IDENTITY_TRANSITION" == stage4-geo/)
  assert.match(prepare, /stage4-geo[\s\S]*RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS false[\s\S]*RISK_IDENTITY_TRUST_CLOUDFLARE_HEADERS true[\s\S]*RISK_IDENTITY_GEO_SOURCE cloudflare_verified/)
	assert.match(prepare, /stage5-composite-enforcement[\s\S]*RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED false[\s\S]*validate_stage5_prerequisites[\s\S]*RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED true/)
	assert.match(apply, /stage5-composite-enforcement[\s\S]*identity\.features\.composite_enforcement/)
  assert.match(apply, /identity\.geo_source == \$geo_source/)
	for (const field of ['configured_rule_count', 'prospective_rule_count', 'effective_rule_count', 'quality_domains', 'gap_sources', 'stale_sources', 'queue_depth', 'dropped']) assert.match(`${prepare}\n${apply}`, new RegExp(field))
	assert.match(apply, /stage3-rules[\s\S]*effective_rule_count >= 5/)
	assert.doesNotMatch(apply, /effective_rule_count >= 1/)
	assert.match(prepare, /validate_stage3_prerequisites/)
  assert.match(ledger, /identity-config/)
  assert.match(ledger, /identity_transition/)
  assert.match(common, /PREPARED_ROOT="\$\{SUB2API_PREPARED_ROOT:-\/var\/lib\/sub2api-release\/prepared\}"/)
  assert.match(common, /RELEASE_BACKUP_ROOT="\$\{SUB2API_RELEASE_BACKUP_ROOT:-\/var\/lib\/sub2api-release\/backups\}"/)
  assert.match(common, /release_ensure_backup_root/)
  assert.match(prepare, /SUB2API_BACKUP_ROOT:-\/var\/lib\/sub2api-release\/backups/)
  assert.match(ledger, /ledger_backup_path_is_protected/)
  assert.match(common, /release_path_chain_has_no_symlink/)
  assert.match(common, /release_prepare_manifest_dir/)
  assert.match(common, /release_install_manifest_files/)
  assert.match(prepare, /release_install_manifest_files/)
  assert.doesNotMatch(prepare, /> "\$MANIFEST_DIR\/manifest\.(?:json|sha256)"/)
  assert.match(apply, /restore_interrupted_base_runtime/)
	assert.match(apply, /"\$UPDATE_KIND" == identity-config && "\$IDENTITY_TRANSITION" != stage0-safe-reset && "\$IDENTITY_TRANSITION" != stage1-\*/)
	assert.match(apply, /"\$IDENTITY_TRANSITION" != stage4-geo/)
	assert.match(apply, /"\$IDENTITY_TRANSITION" != stage5-composite-enforcement/)
	const preSwitch = apply.lastIndexOf("validate_identity_pre_switch || fail_before_mutation")
	const rollbackStart = apply.indexOf('rollback_started=true', preSwitch)
	const switchingExtensions = apply.indexOf('switching_extensions "Beginning extension switch', rollbackStart)
	const checkout = apply.indexOf('release_checkout_exact_commit "$TARGET_COMMIT"', switchingExtensions)
	const install = apply.indexOf('release_install_snapshot_artifacts "$TARGET_DIR"', checkout)
	assert.ok(preSwitch >= 0 && preSwitch < rollbackStart && rollbackStart < switchingExtensions && switchingExtensions < checkout && checkout < install)
})

test('bounded identity delivery exposes enqueue, outcome, drop, and latency metrics', { skip: !identityImplementationPresent }, () => {
  const client = read('backend/internal/service/risk_control_client.go')
  for (const field of ['identityEnqueued', 'identitySucceeded', 'identityFailed', 'identityLatencyNanos', 'identityDropped']) assert.match(client, new RegExp(field))
  for (const key of ['enqueued', 'succeeded', 'failed', 'dropped', 'average_latency_ms']) assert.match(client, new RegExp(`"${key}"`))
})

test('successful API identity observations use daily aggregation, not V1 summaries', { skip: !identityImplementationPresent }, () => {
  const gateway = read('backend/internal/handler/risk_control_gateway.go')
  const schema = read('extensions-self/risk-control/schema.sql')
  const repository = read('extensions-self/risk-control/identity_db.go')
  const defaults = read('extensions-self/risk-control/events.go')
  const ruleContract = read('extensions-self/risk-control/contract.go')
  assert.match(gateway, /client\.IdentityEnabled\(\) && eventType == "api_request"/)
  assert.match(schema, /risk_identity_activity_daily/)
  assert.doesNotMatch(schema, /WHERE code = 'api_request_observation'/)
  assert.doesNotMatch(defaults, /Code: "api_request_observation"/)
  assert.doesNotMatch(ruleContract, /"api_request":\s*\{\}/)
  assert.match(repository, /ActivateShadowRules[\s\S]*api_request_observation/)
  assert.match(repository, /fact\.EventClass == identityEventAPI/)
  assert.match(repository, /risk_identity_api_dedup[\s\S]*ON CONFLICT\(event_key\) DO NOTHING/)
})

test('V2 registration enforces only the composite candidate and fails open on decision-service errors', { skip: !identityImplementationPresent }, () => {
  const registration = read('backend/internal/handler/risk_control_registration.go')
  const gateway = read('backend/internal/handler/risk_control_gateway.go')
  assert.match(registration, /enqueueRiskIdentity\(c, h\.riskControlClient, "registration_attempt"[\s\S]{0,220}IdentityEnabled\(\) \{\s*return nil/)
	assert.match(registration, /IdentityCompositeEnforcementEnabled\(\)[\s\S]*EvaluateIdentityRegistration[\s\S]*identity registration decision failed open[\s\S]*return nil/)
	assert.match(registration, /return riskDecisionError\(decision, "registration"\)/)
  assert.doesNotMatch(registration, /enqueueRiskIdentity\(c, h\.riskControlClient, "login_attempt"[\s\S]{0,220}IdentityEnabled\(\) \{\s*return nil/)
  assert.match(registration, /if err != nil && user != nil && user\.ID > 0 && h\.riskBanHandler != nil/)
  assert.match(registration, /riskControlFailClosed\(\) && !h\.riskControlClient\.IdentityEnabled\(\)/)
  assert.doesNotMatch(gateway, /if client\.IdentityEnabled\(\) \{\s*go reportRiskEvent\(client, input\)\s*return/)
  const admin = read('extensions-self/risk-control/admin.go')
  assert.match(admin, /isRetiredV1IdentityRule/)
})

test('extension migration failure restores the extension without restarting main', { skip: !identityImplementationPresent }, () => {
  const apply = read('deploy/ops/apply-release.sh')

  assert.match(apply, /main_switch_started=false/)
  assert.match(apply, /restore_base_before_main_switch/)
  assert.match(apply, /if \[\[ "\$main_switch_started" == true \]\]; then/)
  assert.match(apply, /release_running_container_matches_image sub2api/)
  assert.match(apply, /main_switch_started=true\s+SUB2API_IMAGE=/)
})

test('custom router observes verification and OAuth stages without provider-file hooks', { skip: !identityImplementationPresent }, () => {
  const router = read('backend/internal/server/custom_router.go')
  const identity = read('backend/internal/handler/risk_control_identity.go')
  assert.match(router, /RiskIdentityAuthLifecycleMiddleware\(customHandlers\.RiskControlClient\)/)
  assert.match(identity, /\/api\/v1\/auth\/send-verify-code/)
  assert.match(identity, /oauth_start/)
  assert.match(identity, /oauth_callback/)
  assert.match(identity, /oauth_completion/)
})

test('successful authentication paths report resolved V2 identity facts', { skip: !identityImplementationPresent }, () => {
  const paths = {
    'backend/internal/handler/auth_handler.go': /reportIdentityLoginSuccess/,
    'backend/internal/handler/auth_email_oauth.go': /reportIdentityLoginSuccess[\s\S]*reportOAuthRegistrationRisk/,
    'backend/internal/handler/auth_oidc_oauth.go': /reportIdentityLoginSuccess[\s\S]*reportOAuthRegistrationRisk/,
    'backend/internal/handler/auth_oauth_pending_flow.go': /reportIdentityLoginSuccess[\s\S]*reportOAuthRegistrationRisk/,
    'backend/internal/handler/auth_linuxdo_oauth.go': /reportIdentityLoginSuccess[\s\S]*reportOAuthRegistrationRisk/,
    'backend/internal/handler/auth_wechat_oauth.go': /reportOAuthRegistrationRisk/,
    'backend/internal/handler/auth_dingtalk_oauth.go': /reportOAuthRegistrationRisk/,
    'backend/internal/handler/passkey_handler.go': /enqueueRiskIdentity\([\s\S]*"passkey_login_success"/,
  }
  for (const [path, contract] of Object.entries(paths)) assert.match(read(path), contract, path)
})

test('V1 login event keys ignore caller-controlled request IDs', { skip: !identityImplementationPresent }, () => {
  const registration = read('backend/internal/handler/risk_control_registration.go')
  assert.equal((registration.match(/requestRiskIdentity\(c, false\)\.EventRoot/g) ?? []).length, 4)
  assert.doesNotMatch(registration, /GetHeader\("X-Request-ID"\)/)
})

test('V1 gateway event keys ignore caller-controlled request IDs', { skip: !identityImplementationPresent }, () => {
  const gateway = read('backend/internal/handler/risk_control_gateway.go')
  assert.match(gateway, /requestID := requestRiskIdentity\(c, false\)\.EventRoot/)
  assert.doesNotMatch(gateway, /GetHeader\("X-Request-ID"\)/)
})

test('API retry dedup and rebuild approval remain atomic for their retention windows', { skip: !identityImplementationPresent }, () => {
  const identityService = read('extensions-self/risk-control/identity_service.go')
  const repository = read('extensions-self/risk-control/identity_db.go')
  assert.match(identityService, /maximumAPISuccessDeliveryAge/)
  assert.match(repository, /BeginTx\(ctx, &sql\.TxOptions\{Isolation: sql\.LevelSerializable\}\)/)
  assert.match(repository, /LOCK TABLE risk_identity_events, risk_identity_rules IN SHARE MODE/)
  assert.match(repository, /WHERE id=\$2 AND dry_run=TRUE AND status='completed' AND requested_by=\$1/)
  const begin = repository.indexOf('BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})')
  const approval = repository.indexOf("WHERE id=$2 AND dry_run=TRUE AND status='completed' AND requested_by=$1")
  const candidates = repository.indexOf('CREATE TEMP TABLE identity_rebuild_candidates')
  assert.ok(begin >= 0 && begin < approval && approval < candidates)
})

test('admin identity signatures bind method, request target, actor, and body', { skip: !identityImplementationPresent }, () => {
  const client = read('backend/internal/service/risk_control_client.go')
  const verifier = read('extensions-self/risk-control/auth.go')
  for (const source of [client, verifier]) {
    assert.match(source, /admin-v2/)
    assert.match(source, /RequestURI\(\)/)
  }
  assert.match(client, /strings\.ToUpper\(method\)/)
  assert.match(verifier, /strings\.ToUpper\(r\.Method\)/)
  assert.match(verifier, /actor[\s\S]*bodyDigest/)
})

test('composite admin evidence matches successful quality-valid V2 facts', { skip: !identityImplementationPresent }, () => {
  const repository = read('extensions-self/risk-control/identity_db.go')

  assert.match(repository, /mine\.outcome='success' AND other\.outcome='success'/)
  assert.match(repository, /mine\.ip_quality_valid AND other\.ip_quality_valid/)
  assert.match(repository, /mine\.device_quality_valid AND other\.device_quality_valid/)
  assert.match(repository, /COUNT\(DISTINCT other_event_id\)::int/)
	assert.match(repository, /window_seconds FROM risk_identity_rules WHERE code='v2_registration_composite_accounts'/)
	assert.match(repository, /ABS\(EXTRACT\(EPOCH FROM\(other\.occurred_at-mine\.occurred_at\)\)\)<=parameters\.overlap_window/)
})

test('identity rules exclude invalid facts and coarse profiles from strong links', { skip: !identityImplementationPresent }, () => {
  const repository = read('extensions-self/risk-control/identity_db.go')

  assert.match(repository, /event_class='registration' AND outcome='success' AND ip_quality_valid AND network_identity_id=\$1/)
	assert.match(repository, /event_class='registration' AND outcome='success' AND device_quality_valid AND browser_identity_id=\$1/)
	assert.match(repository, /outcome='success' AND api_client_identity_id=\$1/)
  assert.match(repository, /outcome='success' AND ip_quality_valid AND device_quality_valid AND network_identity_id=\$1 AND browser_identity_id=\$2/)
  assert.match(repository, /device\.strong && !fact\.DeviceQualityValid/)
  assert.match(repository, /networkID > 0 && fact\.IPQualityValid/)
	assert.ok((repository.match(/identity\.identity_kind IN \('browser_instance','api_client'\)/g) ?? []).length >= 1)
	assert.doesNotMatch(repository, /identity_kind IN \('browser_instance','browser_profile'/)
})

test('admin list identity enrichment is batched and returns masked networks only', { skip: !identityImplementationPresent }, () => {
  const routes = read('backend/internal/server/routes/custom_extensions.go')
  const admin = read('extensions-self/risk-control/identity_admin.go')
  const repository = read('extensions-self/risk-control/identity_db.go')

  assert.match(routes, /admin\.GET\("\/identity-summaries", custom\.AdminUser\.ProxyRiskIdentitySummaries\)/)
  assert.match(admin, /len\(result\) >= 100/)
  assert.match(repository, /UNNEST\(\$1::bigint\[\]\)/)
  assert.match(repository, /bits := 64[\s\S]*address\.Is4\(\)[\s\S]*bits = 24/)
  assert.match(repository, /item\.LatestIP = maskIdentityIP\(plainIP\)/)
})
