# Change Log

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
