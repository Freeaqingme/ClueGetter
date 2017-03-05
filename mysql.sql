SET NAMES utf8;
SET TIME_ZONE='+00:00';
SET character_set_client = utf8;

CREATE TABLE instance (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  name varchar(20) CHARACTER SET ascii NOT NULL,
  description varchar(255) DEFAULT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8;

CREATE TABLE cluegetter_client (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  hostname varchar(127) CHARACTER SET ascii NOT NULL,
  daemon_name varchar(127) CHARACTER SET ascii NOT NULL,
  PRIMARY KEY id (id),
  UNIQUE KEY client (hostname, daemon_name)
) ENGINE=InnoDB;

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
  selector enum('sender','recipient','client_address','sasl_username','sender_domain','recipient_domain', 'sender_sld', 'recipient_sld') NOT NULL,
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

CREATE TABLE quota_profile_period (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  profile bigint(20) unsigned NOT NULL,
  period int(10) unsigned NOT NULL,
  curb int(10) unsigned NOT NULL,
  PRIMARY KEY (id),
  KEY profile (profile),
  CONSTRAINT profile_id FOREIGN KEY (profile) REFERENCES quota_profile (id)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

CREATE TABLE bounce (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  cluegetter_instance bigint(20) unsigned NOT NULL,
  date datetime NOT NULL,
  mta varchar(128) NOT NULL,
  queueId varchar(25) CHARACTER SET ascii NOT NULL,
  messageId varchar(255) NOT NULL COMMENT 'Value of Message-ID header',
  sender varchar(255) NOT NULL,
  PRIMARY KEY (id),
  KEY queueId(queueId),
  KEY cluegetter_instance(cluegetter_instance),
  KEY messageId (messageId),
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
