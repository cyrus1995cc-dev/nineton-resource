-- MySQL 8.4, UTF-8 and UTC. Business dates stay as original strings.

CREATE TABLE IF NOT EXISTS `employees` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(200) NOT NULL DEFAULT '',
  `company` VARCHAR(200) NULL DEFAULT NULL,
  `department` VARCHAR(200) NULL DEFAULT NULL,
  `dingtalk_user_id` VARCHAR(200) NULL DEFAULT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `corp_id` VARCHAR(200) NOT NULL DEFAULT '',
  `union_id` VARCHAR(200) NULL DEFAULT NULL,
  `job_number` VARCHAR(200) NOT NULL DEFAULT '',
  `title` VARCHAR(200) NOT NULL DEFAULT '',
  `department_ids_json` LONGTEXT NOT NULL DEFAULT ('[]'),
  `active` BIGINT NOT NULL DEFAULT 1,
  `missing_since` VARCHAR(200) NULL DEFAULT NULL,
  `synced_at` VARCHAR(200) NULL DEFAULT NULL,
  UNIQUE KEY `idx_employees_0` (`corp_id`,`dingtalk_user_id`),
  UNIQUE KEY `idx_employees_1` (`dingtalk_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `users` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `username` VARCHAR(200) NOT NULL DEFAULT '',
  `password_hash` VARCHAR(200) NOT NULL DEFAULT '',
  `display_name` VARCHAR(200) NOT NULL DEFAULT '',
  `role` VARCHAR(200) NOT NULL DEFAULT 'viewer',
  `dingtalk_user_id` VARCHAR(200) NULL DEFAULT NULL,
  `active` BIGINT NOT NULL DEFAULT 1,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `employee_id` BIGINT NULL,
  UNIQUE KEY `idx_users_0` (`dingtalk_user_id`),
  UNIQUE KEY `idx_users_1` (`employee_id`),
  UNIQUE KEY `idx_users_2` (`dingtalk_user_id`),
  UNIQUE KEY `idx_users_3` (`username`),
  FOREIGN KEY (`employee_id`) REFERENCES `employees` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `sessions` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `user_id` BIGINT NOT NULL,
  `token_hash` VARCHAR(200) NOT NULL DEFAULT '',
  `expires_at` BIGINT NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY `idx_sessions_0` (`token_hash`),
  UNIQUE KEY `idx_sessions_1` (`token_hash`),
  FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `assets` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `asset_code` VARCHAR(200) NOT NULL DEFAULT '',
  `qr_raw` VARCHAR(4000) NULL DEFAULT NULL,
  `qr_format` VARCHAR(200) NULL DEFAULT NULL,
  `category_level1` VARCHAR(200) NULL DEFAULT NULL,
  `category_path` VARCHAR(200) NULL DEFAULT NULL,
  `created_at` VARCHAR(200) NULL DEFAULT NULL,
  `name` VARCHAR(200) NULL DEFAULT NULL,
  `status` VARCHAR(200) NOT NULL DEFAULT '空闲',
  `location` VARCHAR(200) NULL DEFAULT NULL,
  `company` VARCHAR(200) NULL DEFAULT NULL,
  `model` VARCHAR(200) NULL DEFAULT NULL,
  `cpu` VARCHAR(200) NULL DEFAULT NULL,
  `discrete_gpu` VARCHAR(200) NULL DEFAULT NULL,
  `memory` VARCHAR(200) NULL DEFAULT NULL,
  `gpu_model` VARCHAR(200) NULL DEFAULT NULL,
  `serial_number` VARCHAR(200) NULL DEFAULT NULL,
  `remark` TEXT NULL DEFAULT (''),
  `photo_url` TEXT NULL DEFAULT (''),
  `assignee` VARCHAR(200) NULL DEFAULT NULL,
  `department` VARCHAR(200) NULL DEFAULT NULL,
  `received_at` VARCHAR(200) NULL DEFAULT NULL,
  `amount_taxed` DECIMAL(20,4) NOT NULL DEFAULT 0,
  `amount_untaxed` DECIMAL(20,4) NOT NULL DEFAULT 0,
  `tax_amount` DECIMAL(20,4) NOT NULL DEFAULT 0,
  `source_updated_at` VARCHAR(200) NULL DEFAULT NULL,
  `created_at_system` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at_system` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `version` BIGINT NOT NULL DEFAULT 1,
  `assignee_employee_id` BIGINT NULL,
  KEY `idx_assets_0` (`assignee_employee_id`),
  KEY `idx_assets_1` (`company`),
  KEY `idx_assets_2` (`location`),
  KEY `idx_assets_3` (`category_path`),
  KEY `idx_assets_4` (`status`),
  UNIQUE KEY `idx_assets_5` (`asset_code`),
  FOREIGN KEY (`assignee_employee_id`) REFERENCES `employees` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_flows` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `document_no` VARCHAR(200) NOT NULL DEFAULT '',
  `type` VARCHAR(200) NOT NULL DEFAULT '',
  `status` VARCHAR(200) NOT NULL DEFAULT 'pending',
  `applicant_id` BIGINT NOT NULL,
  `applicant_name` VARCHAR(200) NOT NULL DEFAULT '',
  `applicant_department` VARCHAR(200) NOT NULL DEFAULT '',
  `to_assignee` VARCHAR(200) NOT NULL DEFAULT '',
  `to_department` VARCHAR(200) NOT NULL DEFAULT '',
  `to_location` VARCHAR(200) NOT NULL DEFAULT '',
  `note` TEXT NOT NULL DEFAULT (''),
  `current_step` BIGINT NOT NULL DEFAULT 1,
  `request_id` VARCHAR(200) NOT NULL DEFAULT '',
  `request_hash` VARCHAR(200) NOT NULL DEFAULT '',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `completed_at` DATETIME(3) NULL,
  `to_employee_id` BIGINT NULL,
  KEY `idx_asset_flows_0` (`status`,`id`),
  UNIQUE KEY `idx_asset_flows_1` (`request_id`),
  UNIQUE KEY `idx_asset_flows_2` (`document_no`),
  FOREIGN KEY (`to_employee_id`) REFERENCES `employees` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`applicant_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_events` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `asset_id` BIGINT NOT NULL,
  `asset_code` VARCHAR(200) NOT NULL DEFAULT '',
  `type` VARCHAR(200) NOT NULL DEFAULT '',
  `operator_name` VARCHAR(200) NULL DEFAULT NULL,
  `from_status` VARCHAR(200) NULL DEFAULT NULL,
  `to_status` VARCHAR(200) NULL DEFAULT NULL,
  `from_assignee` VARCHAR(200) NULL DEFAULT NULL,
  `to_assignee` VARCHAR(200) NULL DEFAULT NULL,
  `from_department` VARCHAR(200) NULL DEFAULT NULL,
  `to_department` VARCHAR(200) NULL DEFAULT NULL,
  `from_location` VARCHAR(200) NULL DEFAULT NULL,
  `to_location` VARCHAR(200) NULL DEFAULT NULL,
  `note` TEXT NULL DEFAULT (''),
  `request_id` VARCHAR(200) NULL DEFAULT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `flow_id` BIGINT NULL,
  `changes_json` LONGTEXT NOT NULL DEFAULT ('[]'),
  KEY `idx_asset_events_0` (`asset_code`,`created_at`),
  UNIQUE KEY `idx_asset_events_1` (`request_id`),
  FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `import_batches` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `file_name` VARCHAR(200) NULL DEFAULT NULL,
  `checksum` VARCHAR(200) NULL DEFAULT NULL,
  `total_rows` BIGINT NOT NULL DEFAULT 0,
  `new_rows` BIGINT NOT NULL DEFAULT 0,
  `updated_rows` BIGINT NOT NULL DEFAULT 0,
  `unchanged_rows` BIGINT NOT NULL DEFAULT 0,
  `invalid_rows` BIGINT NOT NULL DEFAULT 0,
  `errors_json` LONGTEXT NULL DEFAULT (''),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY `idx_batch_checksum` (`checksum`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `inventory_tasks` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(200) NOT NULL DEFAULT '',
  `location_filter` VARCHAR(200) NULL DEFAULT NULL,
  `department_filter` VARCHAR(200) NULL DEFAULT NULL,
  `status` VARCHAR(200) NOT NULL DEFAULT 'in_progress',
  `operator_name` VARCHAR(200) NULL DEFAULT NULL,
  `total_count` BIGINT NOT NULL DEFAULT 0,
  `checked_count` BIGINT NOT NULL DEFAULT 0,
  `difference_count` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `completed_at` DATETIME(3) NULL,
  KEY `idx_inventory_tasks_0` (`status`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `inventory_records` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `task_id` BIGINT NOT NULL,
  `asset_id` BIGINT NOT NULL,
  `asset_code` VARCHAR(200) NOT NULL DEFAULT '',
  `asset_name` VARCHAR(200) NULL DEFAULT NULL,
  `expected_status` VARCHAR(200) NULL DEFAULT NULL,
  `expected_location` VARCHAR(200) NULL DEFAULT NULL,
  `expected_assignee` VARCHAR(200) NULL DEFAULT NULL,
  `expected_department` VARCHAR(200) NULL DEFAULT NULL,
  `result` VARCHAR(200) NOT NULL DEFAULT 'pending',
  `actual_location` VARCHAR(200) NULL DEFAULT NULL,
  `note` TEXT NULL DEFAULT (''),
  `operator_name` VARCHAR(200) NULL DEFAULT NULL,
  `checked_at` DATETIME(3) NULL,
  KEY `idx_inventory_records_0` (`task_id`,`result`),
  UNIQUE KEY `idx_inventory_records_1` (`task_id`,`asset_id`),
  FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`task_id`) REFERENCES `inventory_tasks` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_flow_items` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `flow_id` BIGINT NOT NULL,
  `asset_id` BIGINT NOT NULL,
  `asset_code` VARCHAR(200) NOT NULL DEFAULT '',
  `asset_name` VARCHAR(200) NOT NULL DEFAULT '',
  `category_path` VARCHAR(200) NOT NULL DEFAULT '',
  `model` VARCHAR(200) NOT NULL DEFAULT '',
  `expected_status` VARCHAR(200) NOT NULL DEFAULT '',
  `expected_location` VARCHAR(200) NOT NULL DEFAULT '',
  `expected_assignee` VARCHAR(200) NOT NULL DEFAULT '',
  `expected_department` VARCHAR(200) NOT NULL DEFAULT '',
  `expected_employee_id` BIGINT NULL,
  KEY `idx_asset_flow_items_0` (`asset_id`,`flow_id`),
  UNIQUE KEY `idx_asset_flow_items_1` (`flow_id`,`asset_id`),
  FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_flow_steps` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `flow_id` BIGINT NOT NULL,
  `sequence` BIGINT NOT NULL,
  `approver_id` BIGINT NOT NULL,
  `approver_name` VARCHAR(200) NOT NULL DEFAULT '',
  `status` VARCHAR(200) NOT NULL DEFAULT '',
  `comment` TEXT NOT NULL DEFAULT (''),
  `acted_at` DATETIME(3) NULL,
  KEY `idx_asset_flow_steps_0` (`approver_id`,`status`),
  UNIQUE KEY `idx_asset_flow_steps_1` (`flow_id`,`sequence`),
  FOREIGN KEY (`approver_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_flow_logs` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `flow_id` BIGINT NOT NULL,
  `action` VARCHAR(200) NOT NULL DEFAULT '',
  `actor_id` BIGINT NOT NULL,
  `actor_name` VARCHAR(200) NOT NULL DEFAULT '',
  `note` TEXT NOT NULL DEFAULT (''),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  FOREIGN KEY (`actor_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_flow_comments` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `flow_id` BIGINT NOT NULL,
  `user_id` BIGINT NOT NULL,
  `user_name` VARCHAR(200) NOT NULL DEFAULT '',
  `content` TEXT NOT NULL DEFAULT (''),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `notification_outbox` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `flow_id` BIGINT NOT NULL,
  `kind` VARCHAR(200) NOT NULL DEFAULT '',
  `recipient_id` BIGINT NOT NULL,
  `recipient_name` VARCHAR(200) NOT NULL DEFAULT '',
  `dedupe_key` VARCHAR(200) NOT NULL DEFAULT '',
  `payload_json` LONGTEXT NOT NULL DEFAULT (''),
  `status` VARCHAR(200) NOT NULL DEFAULT 'pending',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `sent_at` DATETIME(3) NULL,
  KEY `idx_notification_outbox_0` (`status`,`id`),
  UNIQUE KEY `idx_notification_outbox_1` (`dedupe_key`),
  FOREIGN KEY (`recipient_id`) REFERENCES `users` (`id`) ON DELETE NO ACTION,
  FOREIGN KEY (`flow_id`) REFERENCES `asset_flows` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `asset_qr_aliases` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `asset_id` BIGINT NOT NULL,
  `raw` VARCHAR(4000) NOT NULL DEFAULT '',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `raw_hash` BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(`raw`,256))) STORED,
  UNIQUE KEY `idx_asset_qr_aliases_0` (`asset_id`,`raw_hash`),
  FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON DELETE NO ACTION
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `import_previews` (
  `id` VARCHAR(200) NOT NULL PRIMARY KEY,
  `actor_id` BIGINT NOT NULL,
  `file_name` VARCHAR(200) NOT NULL DEFAULT '',
  `rows_json` LONGTEXT NOT NULL DEFAULT (''),
  `versions_json` LONGTEXT NOT NULL DEFAULT (''),
  `expires_at` BIGINT NOT NULL,
  `result_json` LONGTEXT NULL DEFAULT (''),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY `idx_preview_expiry` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `audit_logs` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `actor_id` BIGINT NULL,
  `actor_name` VARCHAR(200) NOT NULL DEFAULT '',
  `action` VARCHAR(200) NOT NULL DEFAULT '',
  `target` VARCHAR(200) NOT NULL DEFAULT '',
  `details_json` LONGTEXT NOT NULL DEFAULT (''),
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `departments` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `corp_id` VARCHAR(200) NOT NULL DEFAULT '',
  `dingtalk_id` BIGINT NOT NULL,
  `parent_id` BIGINT NULL,
  `name` VARCHAR(200) NOT NULL DEFAULT '',
  `synced_at` VARCHAR(200) NOT NULL DEFAULT '',
  UNIQUE KEY `idx_departments_0` (`corp_id`,`dingtalk_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `directory_sync_runs` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `actor_id` BIGINT NULL,
  `status` VARCHAR(200) NOT NULL DEFAULT '',
  `employees_count` BIGINT NOT NULL DEFAULT 0,
  `departments_count` BIGINT NOT NULL DEFAULT 0,
  `message` VARCHAR(200) NOT NULL DEFAULT '',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `completed_at` DATETIME(3) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `system_settings` (
  `key` VARCHAR(200) NOT NULL PRIMARY KEY,
  `value` LONGTEXT NOT NULL DEFAULT ('')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `companies` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `code` VARCHAR(80) NOT NULL,
  `name` VARCHAR(200) NOT NULL,
  `parent_id` BIGINT NULL,
  `remark` VARCHAR(1000) NOT NULL DEFAULT '',
  `dingtalk_corp_id` VARCHAR(200) NULL,
  `dingtalk_department_id` BIGINT NULL,
  `version` BIGINT NOT NULL DEFAULT 1,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY `uq_company_code` (`code`),
  UNIQUE KEY `uq_company_name` (`name`),
  UNIQUE KEY `uq_company_dingtalk` (`dingtalk_corp_id`,`dingtalk_department_id`),
  FOREIGN KEY (`parent_id`) REFERENCES `companies` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `employee_departments` (
  `employee_id` BIGINT NOT NULL,
  `department_id` BIGINT NOT NULL,
  `company_id` BIGINT NULL,
  PRIMARY KEY (`employee_id`,`department_id`),
  KEY `idx_employee_department_company` (`company_id`),
  FOREIGN KEY (`employee_id`) REFERENCES `employees` (`id`),
  FOREIGN KEY (`department_id`) REFERENCES `departments` (`id`),
  FOREIGN KEY (`company_id`) REFERENCES `companies` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS `company_names` (
  `name` VARCHAR(200) NOT NULL PRIMARY KEY,
  `company_id` BIGINT NOT NULL,
  KEY `idx_company_names_company` (`company_id`),
  FOREIGN KEY (`company_id`) REFERENCES `companies` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
