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

test('identity V2 is additive, default-off, and permanently Shadow', { skip: !identityImplementationPresent }, () => {
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
  assert.match(repository, /INSERT INTO risk_identity_signal_history[\s\S]*WHERE domain IN \('ip','device','composite'\)[\s\S]*DELETE FROM risk_identity_signals WHERE domain IN \('ip','device','composite'\)/)
  assert.match(repository, /event\.EventType != "registration_attempt"[\s\S]*email_lookup_key=\$1[\s\S]*domain,rule_code,score[\s\S]*'account'/)
  assert.match(main, /if cfg\.Identity\.RulesEnabled \{[\s\S]*EnsureShadowActivation[\s\S]*ActivateShadowRules/)
  assert.doesNotMatch(schema, /UPDATE risk_rules[\s\S]{0,400}api_request_observation/)
})

test('compose passes independent identity secrets without weak defaults', { skip: !identityImplementationPresent }, () => {
  const compose = `${read('deploy/docker-compose.custom.yml')}\n${read('deploy/docker-compose.custom.local.yml')}`
  for (const key of ['RISK_IDENTITY_V2_ENABLED', 'RISK_IDENTITY_IP_COLLECTION_ENABLED', 'RISK_IDENTITY_DEVICE_COLLECTION_ENABLED', 'RISK_IDENTITY_ADMIN_ENABLED', 'RISK_IDENTITY_RULES_ENABLED', 'RISK_IDENTITY_IP_RULES_ENABLED', 'RISK_IDENTITY_DEVICE_RULES_ENABLED', 'RISK_IDENTITY_COMPOSITE_RULES_ENABLED']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-false\\}`))
  for (const key of ['RISK_IDENTITY_HMAC_KEY', 'RISK_IDENTITY_ENCRYPTION_KEY', 'RISK_DEVICE_COOKIE_SIGNING_KEY', 'RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-\\}`))
  for (const key of ['RISK_DEVICE_COOKIE_SIGNING_KEY_ID', 'RISK_DEVICE_COOKIE_SIGNING_PREVIOUS_KEY_ID']) assert.match(compose, new RegExp(`${key}: \\$\\{${key}:-\\}`))
  assert.doesNotMatch(compose, /RISK_IDENTITY_(?:HMAC|ENCRYPTION)_KEY:.*change_this/)
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

test('V2 registration stays fail-open while non-identity login and API enforcement remain available', { skip: !identityImplementationPresent }, () => {
  const registration = read('backend/internal/handler/risk_control_registration.go')
  const gateway = read('backend/internal/handler/risk_control_gateway.go')
  assert.match(registration, /enqueueRiskIdentity\(c, h\.riskControlClient, "registration_attempt"[\s\S]{0,220}IdentityEnabled\(\) \{\s*return nil/)
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

test('composite admin evidence matches successful quality-valid V2 facts', { skip: !identityImplementationPresent }, () => {
  const repository = read('extensions-self/risk-control/identity_db.go')

  assert.match(repository, /mine\.outcome='success' AND other\.outcome='success'/)
  assert.match(repository, /mine\.ip_quality_valid AND other\.ip_quality_valid/)
  assert.match(repository, /mine\.device_quality_valid AND other\.device_quality_valid/)
  assert.match(repository, /COUNT\(DISTINCT other\.id\)::int/)
})

test('identity rules exclude invalid facts and coarse profiles from strong links', { skip: !identityImplementationPresent }, () => {
  const repository = read('extensions-self/risk-control/identity_db.go')

  assert.match(repository, /event_class='registration' AND outcome='success' AND ip_quality_valid AND network_identity_id=\$1/)
  assert.match(repository, /event_class='registration' AND outcome='success' AND device_quality_valid AND \(browser_identity_id=\$1 OR api_client_identity_id=\$1\)/)
  assert.match(repository, /outcome='success' AND ip_quality_valid AND device_quality_valid AND network_identity_id=\$1 AND browser_identity_id=\$2/)
  assert.match(repository, /device\.strong && !fact\.DeviceQualityValid/)
  assert.match(repository, /networkID > 0 && fact\.IPQualityValid/)
  assert.equal((repository.match(/identity\.identity_kind IN \('browser_instance','api_client'\)/g) ?? []).length, 3)
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
