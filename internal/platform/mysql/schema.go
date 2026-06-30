package mysql

var SchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS feedback (
		id VARCHAR(64) PRIMARY KEY,
		request_id VARCHAR(128) NOT NULL,
		session_id VARCHAR(128) NOT NULL,
		rating VARCHAR(16) NOT NULL,
		reason_tags LONGTEXT NOT NULL,
		` + "`comment`" + ` TEXT NOT NULL,
		corrected_answer LONGTEXT NOT NULL,
		answer_snapshot LONGTEXT NOT NULL,
		evidence_ids LONGTEXT NOT NULL,
		tool_runs LONGTEXT NOT NULL,
		metadata LONGTEXT NOT NULL,
		created_at TIMESTAMP(6) NOT NULL,
		INDEX idx_feedback_request_id (request_id),
		INDEX idx_feedback_session_id (session_id),
		INDEX idx_feedback_created_at (created_at)
	) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`,
	`CREATE TABLE IF NOT EXISTS eval_cases (
		id VARCHAR(64) PRIMARY KEY,
		feedback_id VARCHAR(64) NULL,
		case_type VARCHAR(32) NOT NULL,
		input_message LONGTEXT NOT NULL,
		expected_behavior LONGTEXT NOT NULL,
		gold_answer LONGTEXT NOT NULL,
		forbidden_patterns LONGTEXT NOT NULL,
		metadata LONGTEXT NOT NULL,
		created_at TIMESTAMP(6) NOT NULL,
		INDEX idx_eval_cases_feedback_id (feedback_id),
		INDEX idx_eval_cases_case_type_created_at (case_type, created_at)
	) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci`,
}
