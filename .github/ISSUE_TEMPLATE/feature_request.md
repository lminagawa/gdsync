---
name: Feature request
about: Propose a new behavior, flag, or integration for gdsync.
title: "[feature] "
labels: ["enhancement"]
assignees: []
---

## Problem / motivation

<!--
What are you trying to accomplish that gdsync doesn't help with today?
A concrete workflow example beats an abstract description — e.g. "I work on
two laptops and want to mirror to both OneDrive and a Synology share at once."
-->

## Proposed solution

<!--
What change to gdsync would solve it? New flag? New subcommand? Different
default? Be as specific as you can — even a rough sketch of CLI syntax helps.
-->

## Alternatives considered

<!--
Have you tried doing this with existing flags? With cron + `gdsync --once`?
With a wrapper script? What were the rough edges?
-->

## Out of scope check

gdsync is intentionally narrow. The following are *not* going to be added,
so please don't open requests for them:

- Bi-directional sync (Git is the source of truth, by design).
- Conflict resolution UI (there are no conflicts in a one-way mirror).
- Cloud-provider APIs (gdsync is filesystem-only — that's the point).

If your proposal lives in one of the above areas, consider opening a
[Discussion](https://github.com/lminagawa/gdsync/discussions) instead so the
community can explore the underlying need.

## Additional context

<!-- Links, mockups, prior art from other tools, anything that helps. -->
