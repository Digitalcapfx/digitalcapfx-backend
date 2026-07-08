package handlers

import "time"

// ─── Staff Admin ──────────────────────────────────────────────────────────────

type AuditLogData struct {
	ID         string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	StaffID    string    `json:"staff_id" example:"550e8400-e29b-41d4-a716-446655440001"`
	StaffName  string    `json:"staff_name" example:"Alice Smith"`
	StaffEmail string    `json:"staff_email" example:"alice@digitalfx.com"`
	Action     string    `json:"action" example:"kyc.approve"`
	Resource   string    `json:"resource" example:"kyc"`
	ResourceID string    `json:"resource_id" example:"req-123"`
	Details    any       `json:"details"`
	IPAddress  string    `json:"ip_address" example:"192.168.1.1"`
	CreatedAt  time.Time `json:"created_at" example:"2023-10-12T07:20:50Z"`
}

type AuditLogListResponse struct {
	Logs  []AuditLogData `json:"logs"`
	Total int64          `json:"total" example:"100"`
	Page  int32          `json:"page" example:"1"`
	Limit int32          `json:"limit" example:"50"`
}

type RolePermissionsResponse struct {
	Role        string   `json:"role" example:"admin"`
	Label       string   `json:"label" example:"Administrator"`
	Permissions []string `json:"permissions" example:"users.view,users.edit"`
}