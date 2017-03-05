-- next
DROP TABLE message_body, message_header, message_recipient, message_result, quota_message;
DROP TABLE recipient;
DROP TABLE message;
DROP TABLE session;
DROP TABLE greylist_whitelist;

-- v0.6.2
ALTER TABLE quota CHANGE selector selector enum('sender','recipient','client_address','sasl_username','sender_domain','recipient_domain', 'sender_sld', 'recipient_sld') NOT NULL;

-- v0.5.5
ALTER TABLE quota CHANGE selector selector enum('sender','recipient','client_address','sasl_username','sender_domain','recipient_domain') NOT NULL;
ALTER TABLE message ADD INDEX message_senderdomain_date(sender_domain, date);
ALTER TABLE message DROP INDEX message_sender_domain;
ALTER TABLE recipient ADD UNIQUE INDEX domain (domain, local);
ALTER TABLE recipient DROP INDEX local;

-- v0.5.3
ALTER TABLE bounce ADD messageId varchar(255) NOT NULL COMMENT 'Value of Message-ID header' AFTER queueId;
ALTER TABLE bounce ADD KEY messageId (messageId);

-- v0.5.2
ALTER TABLE session ADD KEY sasl_username (sasl_username);

-- v0.4.3
ALTER TABLE message_header CHANGE name name varbinary(74) not null,
                            CHANGE body body blob not null;

-- v0.4.1
ALTER TABLE session CHANGE date_disconnect date_disconnect datetime DEFAULT NULL;

UPDATE session SET cluegetter_client = (SELECT id FROM cluegetter_client
    WHERE hostname = daemon_name AND daemon_name =
           (SELECT daemon_name FROM cluegetter_client cc2 WHERE cc2.id = session.cluegetter_client))
  WHERE date_connect >= '2015-11-10';
UPDATE cluegetter_client SET daemon_name = 'unknown' WHERE hostname = 'unknown' LIMIT 1;
DELETE FROM cluegetter_client WHERE hostname != daemon_name;

-- v0.4
ALTER TABLE session ADD KEY session_date (cluegetter_instance, date_connect);
ALTER TABLE message ADD KEY message_date_session (date,session);
ALTER TABLE message ADD key message_sender_domain (sender_domain);
ALTER TABLE session ADD helo VARCHAR(255) CHARACTER SET utf8 not null default '' after reverse_dns;
ALTER TABLE message_result CHANGE module module VARCHAR(32) NOT NULL;
LOCK TABLES session WRITE, message WRITE;
ALTER TABLE message DROP FOREIGN KEY message_ibfk_1;
ALTER TABLE session CHANGE id id binary(16) Not NULL;
ALTER TABLE message CHANGE session session binary(16) NOT NULL;
ALTER TABLE message ADD CONSTRAINT message_ibfk_1 FOREIGN KEY (session) REFERENCES session (id);
ALTER TABLE session CHANGE cipher_bits cipher_bits varchar(255) CHARACTER SET ascii default null;
UPDATE session SET cipher_bits = NULL WHERE cipher_bits = '';
ALTER TABLE session CHANGE cipher_bits cipher_bits SMALLINT UNSIGNED DEFAULT NULL;
UNLOCK TABLES;

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
