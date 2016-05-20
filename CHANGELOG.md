# Change Log

### 2016-05-20 Version 0.5.5
* New Feature: Qutoas for sender and recipient domain
* New Feature: Show current quota usage through HTTP interface
* New Feature: Truncate messages sent to ClamAv by default to 10M
* Improvement: Optimized some queries used for http interface
* Change: Use protobuf for RPC communication
* Change: Default SpamAssassin max size now is 500K instead of 8M bytes
* Change: Also include REJECTed messaged when counting quotas


### 2016-04-03 Version 0.5.4
* New Feature: ClamAV integration
* New Feature: Support multiple http frontends
* New Feature: Support Proxy Protocol on the HTTP Interface(s).
* New Feature: Initial Bayes support, train bounced messages as SPAM.
* New Feature: Allow to dump Redis PUBSUB communication to files
* Improvement: Update various libraries (Redis, Mysql)
* Improvement: Update greylist whitelist only once per update interval in entire cluster
* Improvement: Update quotas in Redis only once per update interval in entire cluster
* Improvement: Various Lua touch-ups
* Improvement: Append queue id to all tempfail and reject messages
* Bugfix: Use weighted score for spam-flag score

### 2016-03-25 Version 0.5.3
* New Feature: Initial LUA support
* Improvement: Various changes to cluegetter.service
* Bugfix: Insert headers was broken after first message

### 2016-03-15 Version 0.5.2
* New Feature: Group abusers not just by sender, but also allow to do so using SASL username
* New Feature: Initial Lua support
* Improvement: Allow custom run interval for mail queue scanning
* Improvement: Searching in mail queues is now case insensitive
* Improvement: Include creation of /var/run/cluegetter in systemd service definition
* Improvement: Implement per-session config, allowing for future per-user config settings
* Bugfix: If an error ocurred in a single check, its result was not registered correctly
* Bugfix: send proper queue id to rspamd
* Change: Remove already deprecated add-header %h
* Change: Remove deprecated Add_Header_X_Spam_Score
* Change: Update RDBMS greylist entries every 15 mins instead of every 5 minutes

### 2016-01-24 Version 0.5.1
* New Feature: Implement 'cluegetter bouncehandler submit'
* New Feature: Include Debian packaging in makefile.
* Improvement: Not all queue items have a log_ident field in mailqueue module.
* Improvement: Allow to persist a raw copy of all delivery reports in BounceHandler.
* Improvement: Log that we're about to reopen log files, rather than only afterwards
* Improvement: Don't rely on PATH for postcat & postsuper commands.
* Bugfix: Don't panic if IPC socket can not be connected to.
* Bugfix: Deleting headers could lead to panics if add-headers were given in 'wrong' order.
* Change: Removed Cassandra support. It was still (too) experimental.

### 2016-01-02 Version 0.5.0
* New Feature: Manage MailQueue(s) through single web interface
* New Feature: 'cluegetter log reopen' to facilitate in logrotation.
* Lots of internal clean ups

### 2015-12-23 Version 0.4.4
* New Feature: Initial abuser implementation
* New Feature: Allow to filter on instance in web interface
* New Feature: Allow to embed a Google Analytics tag
* DDL Change: Allow headers to contain unicode (or other, non-ascii) characters
* Improvement: Don't block on importing quotas into redis upon starting
* Bugfix: Quota error message no more shows pointers, but meaningful curbs
* Bugfix: non-regex redis quotas only picked one (random) entry per tuple instead of all
* Changed: No more stats, contained memleak. Will be refactored to statsd

### 2015-11-26 Version 0.4.3
* Bugfix: SpamAssassin default value for timeout and connect timeout were swapped
* Bugfix: Panics are now properly caught, regression introduced in v0.4.2.
* Improvement: Libmilter's error handling behaves differently on FreeBSD than on Linux

### 2015-11-25 Version 0.4.2
* New Feature: Support deletion of headers and made adding of headers more generic
* Bugfix: Properly close all goroutines when breaker score is hit, prevents leaks
* Bugfix: Allow empty message bodies again.
* Improvement: Allow to configure SpamAssassin connect timeout
* Improvement: Add net/http/pprof to web interface
* Improvement: Add date to log format

### 2015-11-16 Version 0.4.1
* Bugfix: Correctly register MtaHostName, rather than using PTR
* Bugfix: Prevent potential race condition in quotasRedisPollQuotasBySelector()
* Bugfix: Quotas in RDBMS should not fail when message is persisted asynchronously
* Improvement: Gracefully recover in case a message callback fails

### 2015-11-16 Version 0.4
* New Feature: Redis integration: Optionally use Redis for quotas greylisting, and asynchronous persistence.
* Improvement: Updated some queries and indexes to improve database performance significantly.
* New Feature: Experimental Cassandra support.

### 2015-11-04 Version 0.3.4
* New Feature: Allow to configure pruning interval, or disable pruning altogether.

### 2015-10-05 Version 0.3.3
* New Feature: Rspamd Integration
* New Feature: Module Groups
* Improvement: Prune message bodies using mysql indexes. Did not work on mysql <5.7 due to a mysql bug.
* Improvement: Be able to actually handle ELHO/HELO commands mid-session and RSET commands.
* Schema Change: Add rspamd module, error verdict message_result
* Bugfix: Body size was displayed incorrectly with bodies >65 KiB
* Bugfix: Only prune sessions older than X weeks
* Bugfix: Improve constructing of message to SpamAssassin

### 2015-09-20 Version 0.3.2
* New Feature: Allow to add hostname to static headers
* New Feature: Insert message-id header when it's missing
* New Feature: Truncate messages as they're sent to SpamAssassin
* New Feature: Prune data when it's been in the database for too long
* New Feature: Register and show time taken per module
* New Feature: Store & Display body size, ciphers, auth method used, MTA, reverse dns, etc
* Change: Set Mysql isolation level to READ-UNCOMMITTED

### 2015-09-15 Version 0.3.1
* New Feature: Allow to log to a file instead of just STDOUT/STDERR
* Bugfix: SpamAssassin module would not work with Golang 1.5

### 2015-09-13 Version 0.3
* New Feature: Bounce Handler, used to keep track of bounces
* New Feature: Allow to search by Domain, IP and SASL User in web interface
* New Feature: Insert x-spam-score headers (optional)
* New Feature: Allow to insert static header lines
* Improvement: Only load modules if they're actually enabled
* Improvement: Round scores in html frontend to two digits
* Bugfix: Add missing received-by header for SpamAssassin so it can determine the correct ip

### 2015-09-10 Version 0.2.6
* Bugfix: Allow nullsenders (From: <>)
* Improvement: Add additional locking around stats, preventing race conditions
* Schema change: Allow for longer local parts than RFC suggests
* Schema change: change address field from varbinary(16) to varchar(45)

### 2015-09-01 Version 0.2.5
* Bugfix: Allow quota and greylist module to run concurrently
* Bugfix: Email addresses should be case insensitive, prevent duplicate key errors in quota module
* Change: Only display stats every 180 secs rather than every 60 secs

### 2015-08-30 Version 0.2.4
* New feature: Allow to specify a whitelist for greylists based on SPF records

### 2015-08-25 Version 0.2.3
* New feature: Whitelisting
* Bugfix: Bugfix: Don't prepare new db prepared statement for each new session

### 2015-08-06 Version 0.2.2
* New feature: Greylisting
* New feature: Added exit-on-panic config directive, giving more accurate stack traces
* New Feature: Quota classes, allow to group quota profiles
* Bugfix: Don't panic on messages that has the same recipient multiple times
* Build change: Assets are now compiled into the binary (when making a production build)

### 2015-07-19 Version 0.2.1
* Schema change: save body in chunks in separate table
* Added a makefile to create binary.
* Assets (like html templates) are now built into binary. No more need to install separately
* RDBMS password is no more displayed when exiting
* Parse email addresses in form '<foo@bar>' as 'foo@bar'
* Fixed some race conditions in error handling

### 2015-06-13 First release, version 0.2
* Quota support
* SpamAssassin integration
* HTTP Interface
