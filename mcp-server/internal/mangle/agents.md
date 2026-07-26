# Mangle Engine

The engine provides bounded facts, complete clause-body queries, isolated
untrusted rule evaluation, adaptive sampling, temporal queries, and fresh-fact
subscriptions.

All user programs must honor configured byte, clause, premise, rule,
created-fact, result, and wall-clock limits. A timeout or panic is an explicit
error. Subscriptions establish a baseline and do not fire on prior history.
High-value causal facts and correlated network pairs must survive sampling.

`schema_loader.go` owns file, directory, and confined manifest discovery.
Canonical declarations live in `../../schemas/modules/`; `browser.mg` is the
compatibility manifest. Module load order is lexical and parse errors must name
the source module.
