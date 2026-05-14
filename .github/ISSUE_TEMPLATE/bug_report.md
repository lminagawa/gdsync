---
name: Bug report
about: Something didn't sync, got corrupted, or crashed — let's fix it.
title: "[bug] "
labels: ["bug"]
assignees: []
---

## Summary

<!-- One sentence: what went wrong? -->

## Expected behavior

<!-- What did you think gdsync would do? -->

## Actual behavior

<!-- What actually happened? Include exact error messages if any. -->

## Reproduction

```sh
# The exact gdsync command you ran, including flags:
gdsync --dest ... [other flags]

# What you did in the source tree to trigger the problem (touch / rm / git reset / etc.):
```

If the bug involves specific files or directory shapes, please describe the
relevant parts of the source tree (no need to share private content — a sketch
like `repo/sub/foo.txt (symlink → bar.txt)` is plenty).

## Environment

- **gdsync version**: <!-- output of `gdsync version` -->
- **OS**: <!-- e.g. macOS 14.4 (arm64), Windows 11 23H2 (amd64) -->
- **Go version (if built from source)**: <!-- `go version` -->
- **Cloud sync client (if relevant)**: <!-- OneDrive / Google Drive / Dropbox / iCloud / none -->
- **Filesystem of `--dest`**: <!-- APFS / NTFS / exFAT / etc. -->

## Logs

Run with `-v` and paste the relevant lines here. Please redact any paths that
contain personal information.

```
<paste log output>
```

## Additional context

<!-- Anything else: screenshots, related issues, hunches about the cause. -->

---

<!--
Before submitting, please check:
- [ ] I can reproduce this with the latest gdsync release.
- [ ] My `--dest` is a *local* filesystem path (not a network drive that's flaking).
- [ ] No other process is actively holding the destination files.
-->
