# ClueGetter - Access and Auditing Milter for Postfix

Cluegetter provides a means to have an integrated way to determine if a message
should be accepted by Postfix. All email (metadata) and verdicts are stored in a
database allowing for auditing.

Each message has a verdict of one of the following values:
* Permit: Accept message.
* Tempfail: Deny delivery, but expect the delivering MTA to deliver it at a later time.
* Reject: Reject the message, indicating it will not be accepted a next time either.

Current verdict determining modules:
* Quotas
* SpamAssassin

Planned modules:
* ClamAv/Clamd - Scan the message for viruses
* Greylisting
* Mailqueue - See if/how many messages are stuck in the mail queue

ClueGetter should be usable, but as long as no 1.0 release has been released,
you should make sure to test it before using in production. Coming to think
of it, you should always test anything you take into production. But at
least you've been warned.

See the screenshots directory to get some ideas on what the HTTP interface
looks like.

## Changelog

#### 2015-06-13 First release, version 0.2
Key features:
* Quota support
* SpamAssassin integration
* HTTP Interface

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
cluegetter --config ./cluegetter.conf --loglevel=DEBUG
```

## Quotas
The quotas module allows to set arbitrary limits on various factors, where the
limits can be different per (predefined) factor value.

Currently supported factors:
* Client Address: The IP of the connecting client.
* Sender: The email address of the party sending the email.
* Recipient: The email address of the recipient.
* Sasl Username: The SASL Username that was used to connect to.

Each combination of predefined factor and factor value (stored in the *quota*
table) is assigned a quota profile. Next, a quota profile has one or more profile
periods. These periods determine the maximum amount of messages accepted over
that period.

For example, say you're an ESP that has two offerings (packages *large* and
*small*) and you're using SASL authentication. Your user *john@doe.com*
has the small package, the *jane@doe.com* SASL user pays for the large
package. With the *large* package you can send 500 emails per 5 minutes, and
a total of 10.000 per 24 hours. The *small* package allows for a total of 150
messages per 24 hours.

To implement this scenario you'd make sure your database contains the following
entries.
```
quota:
+-----+----------------+----------------+----------+---------+------------+
| id  | selector       | value          | is_regex | profile | instigator |
+-----+----------------+----------------+----------+---------+------------+
|   1 | sasl_username  | john@doe.com   |        0 |       1 |       NULL |
|   2 | sasl_username  | jane@doe.com   |        0 |       2 |       NULL |
+-----+----------------+----------------+----------+---------+------------+

quota_profile:
+----+--------------------+
| id | name               |
+----+--------------------+
|  1 | small-sasl         |
|  2 | large-sasl         |
+----+--------------------+

quota_profile_period:
+----+---------+--------+-------+
| id | profile | period | curb  |
+----+---------+--------+-------+
|  1 |       1 |  86400 |   150 |
|  2 |       2 |    300 |   500 |
|  3 |       2 |  86400 | 10000 |
+----+---------+--------+-------+
```

### Regexes
Some times it's not possible to know all the factor values that you need a quota
for in advance. For example, when you want to do rate limiting based on IP
addresses. For this reason, you can configure use a regex in quota table,

That could look like this:
```
+-----+----------------+----------------+----------+---------+------------+
| id  | selector       | value          | is_regex | profile | instigator |
+-----+----------------+----------------+----------+---------+------------+
|   1 | client_address | 127.0.0.1      |        0 |       3 |       NULL |
|   2 | client_address | ^.*$           |        1 |       4 |       NULL |
+-----+----------------+----------------+----------+---------+------------+
```

Now, assuming you also enabled *quotas.account-client-address* in the
configuration. Whenever a message comes in, ClueGetter will first check if there
is a row where:
```selector = 'client_address' AND value = '<IP>' AND is_regex = 0```

If there is no such row, it will starts to check for rows where
```selector = 'client_address' AND is_regex = 1```

In case there is a message from an IP not seen before, the quota table will look
as follows after processing it:
```
+-----+----------------+----------------+----------+---------+------------+
| id  | selector       | value          | is_regex | profile | instigator |
+-----+----------------+----------------+----------+---------+------------+
|   1 | client_address | 127.0.0.1      |        0 |       3 |       NULL |
|   2 | client_address | ^.*$           |        1 |       4 |       NULL |
|   3 | client_address | 127.0.0.128    |        0 |       4 |          2 |
+-----+----------------+----------------+----------+---------+------------+
```

## License

ClueGetter is distributed under a BSD 2-clause style license.
Please the *LICENSE* file for specifics.