# Changelog

## 0.2.0

- Add the `call.billed` webhook event (`EventCallBilled`), parsed as `*CallEvent`. It is sent once per call after
  `call.completed`, when the final billing record is applied; `Call.Price` and `Call.BilledDuration` are final.

## 0.1.0

Initial release: calls, balance, pricing, webhooks.
