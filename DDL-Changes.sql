-- v0.3.5
ALTER TABLE session ADD KEY session_date (cluegetter_instance, date_connect);
ALTER TABLE message ADD KEY message_date_session (date,session);
ALTER TABLE message add key message_sender_domain (sender_domain);

-- V0.3.3
ALTER TABLE message CHANGE body_size body_size int unsigned DEFAULT NULL ;
ALTER TABLE message_result CHANGE module module enum('quotas','spamassassin','rspamd','greylisting') NOT NULL;
ALTER TABLE message_result CHANGE verdict verdict enum('permit', 'tempfail', 'reject', 'error') NOT NULL;
ALTER TABLE message_result ADD weighted_score float(6,2) DEFAULT NULL AFTER score;
UPDATE message_result SET weighted_score = score;

-- V0.3.2

CREATE TABLE cluegetter_client (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  hostname varchar(127) CHARACTER SET ascii NOT NULL,
  daemon_name varchar(127) CHARACTER SET ascii NOT NULL,
  PRIMARY KEY id (id),
  UNIQUE KEY client (hostname, daemon_name)
) ENGINE=InnoDB;
INSERT INTO cluegetter_client (id, hostname, daemon_name) VALUES(1, 'unknown', '');
ALTER TABLE session ADD cluegetter_client bigint(20) unsigned NOT NULL AFTER cluegetter_instance;
UPDATE session SET cluegetter_client = 1;
ALTER TABLE session ADD CONSTRAINT session_ibfk_2 FOREIGN KEY (cluegetter_client) REFERENCES cluegetter_client (id);

ALTER TABLE session ADD reverse_dns varchar(255) CHARSET utf8 NOT NULL DEFAULT '' AFTER ip;
ALTER TABLE session ADD sasl_method varchar(32) NOT NULL DEFAULT '';
ALTER TABLE session ADD cert_issuer varchar(255) charset ascii NOT NULL DEFAULT '';
ALTER TABLE session ADD cert_subject varchar(255) charset ascii NOT NULL DEFAULT '';
ALTER TABLE session ADD cipher_bits varchar(255) charset ascii NOT NULL DEFAULT '';
ALTER TABLE session ADD cipher varchar(255) charset ascii NOT NULL DEFAULT '';
ALTER TABLE session ADD tls_version varchar(31) charset ascii NOT NULL DEFAULT '';

ALTER TABLE message ADD body_size INT UNSIGNED NOT NULL AFTER date;
ALTER TABLE message ADD body_hash char(32) DEFAULT '' AFTER body_size;
ALTER TABLE message ADD rejectScoreThreshold float(6,2) DEFAULT NULL AFTER rejectScore;
ALTER TABLE message ADD tempfailScoreThreshold float(6,2) DEFAULT NULL AFTER tempfailScore;
UPDATE message SET rejectScoreThreshold = 5;
UPDATE message SET tempfailScoreThreshold = 8;

DELETE FROM message_recipient WHERE NOT EXISTS (SELECT * FROM message WHERE message_recipient.message = message.id);
ALTER TABLE message_recipient ADD CONSTRAINT message_recipient_ibfk_1 FOREIGN KEY (message) REFERENCES message (id);

ALTER TABLE message_result ADD duration float(6,3) COMMENT 'in seconds' AFTER score;
