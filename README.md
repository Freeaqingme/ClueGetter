# ClueGetter - *Does things with mail*

Provides Quota and Greylisting support for Postfix.

This document needs some more love. For now some random notes:
* Postfix should be configured to use:
```
  smtpd_milters = inet:localhost:10033
  enable_long_queue_ids = yes
  ```
