# BrowserNERD

BrowserNERD gives agents reliable, compact evidence about what a person can do
in a browser, what happened when they acted, and why a flow failed.

## General-purpose mandate

Every BrowserNERD feature must be general purpose and usable with any web
application. Its capabilities must not depend on a particular app, framework,
repository, vendor, operating system, deployment topology, or service name.

Application-specific knowledge belongs in user or workspace configuration,
configured spec corpora, and test fixtures. Core code, shipped defaults, and
public interfaces must not hardcode an application's routes, credentials,
tenant model, directory layout, log locations, or business rules. Configured
adapters may specialize how evidence is collected while preserving the same
application-independent reasoning and reporting contract.

Each feature must preserve this boundary in its implementation, documentation,
and verification. Prove reusable behavior with generic applications and
fixtures; an integration with one app is an example of the capability, not its
definition.
