# ClueGetter - Access and Auditing Milter

Cluegetter provides a means to have an integrated way to determine if a message
should be accepted by Postfix. All email (metadata) and verdicts are stored in a
database allowing for auditing.

Each message has a verdict of one of the following values:
* Permit: Accept message.
* Tempfail: Deny delivery, but expect the delivering MTA to deliver it at a later time.
* Reject: Reject the message, indicating it will not be accepted a next time either.

Features:
* [Quotas](https://github.com/Freeaqingme/ClueGetter/wiki/Quotas) - Limit the number of emails
  (per ip, sasl user, recipient, sender) over an arbitrary amount of time. 
* SpamAssassin - Determine whether an email is SPAM through [SpamAssassin](http://spamassassin.apache.org/).
  Can be used alongside the Rspamd module.
* Rspamd - Determine whether an email is SPAM through [Rspamd](http://www.rspamd.com).
  Can be used alongside the SpamAssassin module.
* Greylisting - Ask a server to try again in a bit if it wasn't seen before and the mail looks spammy.
* [Bounce Handling](https://github.com/Freeaqingme/ClueGetter/wiki/Bounce-Handling) - Keep track of
  what emails were rejected by remote MTAs and for what reasons.
* Abusers - Present a list of users in the web interface who had an unusual amount of email rejected.
  Usually these users have been hacked, or are otherwise malicious.
* MailQueue - Display an aggregate of all mail queues that reside in your ClueGetter cluster.
  Filter based on instance, recipient(/domain), sender(/domain) and delete or requeue selections
  of items in the queue.

Planned modules:
* GeoIP - Detect anomalies in the countries used to send mail from
* ClamAv/Clamd - Scan the message for viruses
* Reputation - Incorporate previous verdicts in future verdicts.
* SRS - Implement with proper support for virtual domains

See the [Wiki](https://github.com/Freeaqingme/ClueGetter/wiki) for more documentation how to use
these features.

ClueGetter should be usable, but as long as no 1.0 release has been released,
you should make sure to test it before using in production. Coming to think
of it, you should always test anything you take into production. But at
least you've been warned.


## Screenshots

| Message Details | Searching for a message | Search results | Mail Queue |
| ------------- | ------------- | ------------- | ------------- | 
| [![Message Details](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/thumbs/200.MessageDetails.png)](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/MessageDetails.png) | [![Search for a message](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/thumbs/200.Search.png)](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/Search.png) | [![Search Results](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/thumbs/200.SearchResultsByEmail.png)](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/SearchResultsByEmail.png) | [![Mail Queue ](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/thumbs/200.MailQueue.png)](https://raw.githubusercontent.com/Freeaqingme/ClueGetter/develop/screenshots/MailQueue.png) |



## Quick Setup
Copy the example config file:
```cp cluegetter.conf.dist cluegetter.conf```

Add the following directives to Postfix' main.cf:
```
smtpd_milters = inet:localhost:10033
enable_long_queue_ids = yes
  ```

The long queue id's are necessary because ClueGetter uses these id's as internal
reference and as such they are required to be unique (which the
*enable_long_queue_ids* directive ensures).

If you want to test ClueGetter first to see how it would behave, without actually
influencing current operations, run it in noop mode.

Change the *noop* directive in the cluegetter config file:
```
noop = true
  ```
Add to the Postfix main.cf:
```
milter_default_action=accept
```

Create and fill the database:
```
echo 'CREATE DATABASE cluegetter DEFAULT CHARACTER SET utf8' | mysql
mysql cluegetter < mysql.sql
```

Run ClueGetter:
```
make
./bin/cluegetter --config ./cluegetter.conf --loglevel=DEBUG daemon --foreground
```

Once you got things up and running, consider setting up Redis. This will
significantly improve performance and the ability to handle email while
under load.

## License

ClueGetter is distributed under a BSD 2-clause style license.
Please see the *LICENSE* file for specifics.
