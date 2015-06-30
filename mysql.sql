SET NAMES utf8;
SET TIME_ZONE='+00:00';
SET character_set_client = utf8;

CREATE TABLE instance (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  name varchar(20) CHARACTER SET ascii NOT NULL,
  description varchar(255) DEFAULT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE session (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  date_connect datetime NOT NULL,
  date_disconnect datetime DEFAULT NULL,
  ip varchar(45) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
  sasl_username varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY cluegetter_instance (cluegetter_instance),
  CONSTRAINT session_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message (
  id varchar(25) CHARACTER SET ascii NOT NULL,
  session bigint(20) unsigned NOT NULL,
  date datetime NOT NULL,
  messageId varchar(255) NOT NULL COMMENT 'Value of Message-ID header',
  sender_local varchar(64) NOT NULL,
  sender_domain varchar(253) NOT NULL,
  body longtext,
  rcpt_count int(10) unsigned NOT NULL DEFAULT '1',
  verdict enum('permit','tempfail','reject') DEFAULT NULL,
  verdict_msg text,
  rejectScore float(6,2) DEFAULT NULL,
  tempfailScore float(6,2) DEFAULT NULL,
  PRIMARY KEY (id),
  KEY session (session),
  KEY messageId (messageId(25)),
  CONSTRAINT message_ibfk_1 FOREIGN KEY (session) REFERENCES session (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

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
  PRIMARY KEY (message,recipient)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE message_result (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  message varchar(25) CHARACTER SET ascii NOT NULL,
  module enum('quotas','spamassassin','greylist','clamav') NOT NULL,
  verdict enum('permit','tempfail','reject') NOT NULL,
  score float(6,2) DEFAULT NULL,
  determinants text CHARACTER SET ascii COMMENT 'JSON',
  PRIMARY KEY (id),
  UNIQUE KEY message (message,module),
  CONSTRAINT message_result_ibfk_1 FOREIGN KEY (message) REFERENCES message (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE quota_profile (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  name varchar(32) NOT NULL,
  PRIMARY KEY (id),
  KEY cluegetter_instance (cluegetter_instance),
  CONSTRAINT quota_profile_ibfk_1 FOREIGN KEY (cluegetter_instance) REFERENCES instance (id)
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
  local varchar(64) DEFAULT NULL,
  domain varchar(253) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY local (local,domain)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

INSERT INTO instance (id, name, description) VALUES(1, 'default', 'The Default Instance');
INSERT INTO quota_profile (id, cluegetter_instance, name) VALUES (1, 1, 'Trusted');
INSERT INTO quota_profile (id, cluegetter_instance, name) VALUES (2, 1, 'Villains');
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(1, 1, 300, 500);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(2, 1, 3600, 1000);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(3, 1, 86400, 10000);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(4, 2, 300, 150);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(5, 2, 3600, 500);
INSERT INTO quota_profile_period (id, profile, period, curb) VALUES(6, 2, 86400, 1000);
INSERT INTO quota (id, selector, value, profile, is_regex, date_added) VALUES (1, 'client_address', '::1',  1, 0, NOW());
INSERT INTO quota (id, selector, value, profile, is_regex, date_added) VALUES (2, 'client_address', '^.*$', 2, 1, NOW());
