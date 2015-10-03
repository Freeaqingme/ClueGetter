SET NAMES utf8;
SET TIME_ZONE='+00:00';
SET character_set_client = utf8;

CREATE TABLE instance (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  name varchar(20) CHARACTER SET ascii NOT NULL,
  description varchar(255) DEFAULT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE greylist_whitelist (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  ip varchar(45) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  last_seen datetime DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY cluegetter_instance (cluegetter_instance,ip),
  CONSTRAINT greylist_whitelist_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE cluegetter_client (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  hostname varchar(127) CHARACTER SET ascii NOT NULL,
  daemon_name varchar(127) CHARACTER SET ascii NOT NULL,
  PRIMARY KEY id (id),
  UNIQUE KEY client (hostname, daemon_name)
) ENGINE=InnoDB;

CREATE TABLE session (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  cluegetter_client bigint(20) unsigned NOT NULL,
  date_connect datetime NOT NULL,
  date_disconnect datetime DEFAULT NULL,
  ip varchar(45) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
  reverse_dns varchar(255) CHARSET utf8 NOT NULL DEFAULT '',
  sasl_username varchar(255) NOT NULL DEFAULT '',
  sasl_method varchar(32) NOT NULL DEFAULT '',
  cert_issuer varchar(255) charset ascii NOT NULL DEFAULT '',
  cert_subject varchar(255) charset ascii NOT NULL DEFAULT '',
  cipher_bits varchar(255) charset ascii NOT NULL DEFAULT '',
  cipher varchar(255) charset ascii NOT NULL DEFAULT '',
  tls_version varchar(31) charset ascii NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY cluegetter_instance (cluegetter_instance),
  CONSTRAINT session_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id),
  CONSTRAINT session_ibfk_2 FOREIGN KEY (cluegetter_client) REFERENCES cluegetter_client (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message (
  id varchar(25) CHARACTER SET ascii NOT NULL,
  session bigint(20) unsigned NOT NULL,
  date datetime NOT NULL,
  body_size int unsigned DEFAULT NULL,
  body_hash char(32) DEFAULT '',
  messageId varchar(255) NOT NULL COMMENT 'Value of Message-ID header',
  sender_local varchar(255) NOT NULL,
  sender_domain varchar(253) NOT NULL,
  rcpt_count int(10) unsigned NOT NULL DEFAULT 1,
  verdict enum('permit','tempfail','reject') DEFAULT NULL,
  verdict_msg text,
  rejectScore float(6,2) DEFAULT NULL,
  rejectScoreThreshold float(6,2) DEFAULT NULL,
  tempfailScore float(6,2) DEFAULT NULL,
  tempfailScoreThreshold float(6,2) DEFAULT NULL,
  PRIMARY KEY (id),
  KEY session (session),
  KEY messageId (messageId(25)),
  CONSTRAINT message_ibfk_1 FOREIGN KEY (session) REFERENCES session (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message_body (
  message varchar(25) CHARACTER SET ascii NOT NULL,
  sequence smallint(5) unsigned NOT NULL,
  body mediumblob not null,
  PRIMARY KEY (message, sequence),
  CONSTRAINT message_body_ibfk_1 FOREIGN KEY (message) REFERENCES message (id)
) ENGINE=InnoDB;

CREATE TABLE message_header (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  message varchar(25) CHARACTER SET ascii NOT NULL,
  name varchar(74) CHARACTER SET ascii DEFAULT NULL,
  body text CHARACTER SET ascii,
  PRIMARY KEY (id),
  KEY message (message),
  CONSTRAINT message_header_ibfk_1 FOREIGN KEY (message) REFERENCES message (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message_recipient (
  message varchar(25) CHARACTER SET ascii NOT NULL,
  recipient bigint(20) unsigned NOT NULL,
  count smallint(5) unsigned NOT NULL DEFAULT 1,
  PRIMARY KEY (message,recipient),
  CONSTRAINT message_recipient_ibfk_1 FOREIGN KEY (message) REFERENCES message (id),
  CONSTRAINT message_recipient_ibfk_2 FOREIGN KEY (recipient) REFERENCES recipient (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message_result (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  message varchar(25) CHARACTER SET ascii NOT NULL,
  module enum('quotas','spamassassin','clamav','greylisting') NOT NULL,
  verdict enum('permit','tempfail','reject') NOT NULL,
  score float(6,2) DEFAULT NULL,
  duration float(6,3) COMMENT 'in seconds',
  determinants text CHARACTER SET ascii COMMENT 'JSON',
  PRIMARY KEY (id),
  UNIQUE KEY message (message,module),
  CONSTRAINT message_result_ibfk_1 FOREIGN KEY (message) REFERENCES message (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE quota_class (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  name varchar(32) NOT NULL,
  PRIMARY KEY (id),
  KEY cluegetter_instance (cluegetter_instance),
  CONSTRAINT quota_class_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE quota_profile (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  class bigint(20) unsigned NOT NULL,
  name varchar(32) NOT NULL,
  PRIMARY KEY (id),
  KEY class (class),
  CONSTRAINT quota_profile_ibfk_1 FOREIGN KEY (class) REFERENCES quota_class (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE quota (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  selector enum('sender','recipient','client_address','sasl_username') NOT NULL,
  value varchar(255) DEFAULT NULL,
  is_regex tinyint(1) DEFAULT '0',
  profile bigint(20) unsigned NOT NULL,
  instigator bigint(20) unsigned DEFAULT NULL,
  date_added datetime NOT NULL,
  PRIMARY KEY id (id),
  UNIQUE KEY selector (selector,value,profile),
  KEY profile (profile),
  KEY selector_value (selector,value),
  CONSTRAINT quota_ibfk_1 FOREIGN KEY (profile) REFERENCES quota_profile (id)
) ENGINE=InnoDB DEFAULT CHARSET=ascii;

CREATE TABLE quota_message (
  quota bigint(20) unsigned NOT NULL,
  message varchar(25) CHARACTER SET ascii NOT NULL,
  PRIMARY KEY (quota,message),
  KEY message (message),
  CONSTRAINT quota_message_ibfk_1 FOREIGN KEY (quota) REFERENCES quota (id),
  CONSTRAINT quota_message_ibfk_2 FOREIGN KEY (message) REFERENCES message (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE quota_profile_period (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  profile bigint(20) unsigned NOT NULL,
  period int(10) unsigned NOT NULL,
  curb int(10) unsigned NOT NULL,
  PRIMARY KEY (id),
  KEY profile (profile),
  CONSTRAINT profile_id FOREIGN KEY (profile) REFERENCES quota_profile (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE recipient (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  local varchar(255) NOT NULL,
  domain varchar(253) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY local (local,domain)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE bounce (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  date datetime NOT NULL,
  mta varchar(128) NOT NULL,
  queueId varchar(25) CHARACTER SET ascii NOT NULL,
  sender varchar(255) NOT NULL,
  PRIMARY KEY (id),
  KEY queueId(queueId),
  KEY cluegetter_instance(cluegetter_instance),
  CONSTRAINT bounce_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE bounce_report (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  bounce bigint(20) unsigned NOT NULL,
  status varchar(16) NOT NULL,
  orig_rcpt varchar(255) NOT NULL,
  final_rcpt varchar(255) NOT NULL,
  remote_mta varchar(128) NOT NULL,
  diag_code text,
  PRIMARY KEY (id),
  CONSTRAINT bounce_report_ibfk_1 FOREIGN KEY (bounce) REFERENCES bounce (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO instance (id, name, description) VALUES (1, 'default', 'The Default Instance');
INSERT INTO quota_class (id, cluegetter_instance, name) VALUES (1, 1, 'Trusted');
INSERT INTO quota_class (id, cluegetter_instance, name) VALUES (2, 1, 'Villains');
INSERT INTO quota_profile (id, class, name) VALUES (1, 1, 'Low Volume');
INSERT INTO quota_profile (id, class, name) VALUES (2, 1, 'Mid Volume');
INSERT INTO quota_profile (id, class, name) VALUES (3, 1, 'High Volume');
INSERT INTO quota_profile (id, class, name) VALUES (4, 2, 'Limited Volume');
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(1, 1, 300, 5);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(2, 1, 3600, 10);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(3, 1, 86400, 100);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(4, 2, 300, 50);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(5, 2, 3600, 100);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(6, 2, 86400, 1000);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(7, 3, 300, 500);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(8, 3, 3600, 1000);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(9, 3, 86400, 10000);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(10, 4, 300, 15);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(11, 4, 3600, 50);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(12, 4, 86400, 100);
INSERT INTO quota (id, selector, value, profile, is_regex, date_added) VALUES (1, 'client_address', '::1',  1, 0, NOW());
INSERT INTO quota (id, selector, value, profile, is_regex, date_added) VALUES (2, 'client_address', '^.*$', 4, 1, NOW());
