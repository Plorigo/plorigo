-- name: CreateAuditEvent :one
INSERT INTO audit_events (workspace_id, actor, action, target_type, target_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, workspace_id, actor, action, target_type, target_id, created_at;
