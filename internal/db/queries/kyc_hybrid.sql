-- name: SetKycProviderStatus :exec
-- Records the KYC provider's latest automated decision (does not touch the
-- final kyc_status).
UPDATE users SET kyc_provider_status = $2, updated_at = now() WHERE id = $1;

-- name: SetKycManualOverride :exec
-- Flags (or clears) admin manual control over a user's KYC decision.
UPDATE users SET kyc_manual_override = $2, updated_at = now() WHERE id = $1;
