# Change Log

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
