# Changelog

One section per release, the heading is the version. `.github/workflows/release.yml`
takes the section of the tag being released and puts the Chinese one below
`<!-- lang:zh -->`; the app shows whichever language the interface is in, so write
this for users, not as a list of commits.

Format: `## <version>` to start a section (no `v`); it runs until the next `## `.

## 0.2.1

This release gives every setting the same card row, and lets Papero update itself
straight from GitHub.

### Updating

- The About page has a **Check for updates** row: a new version downloads, is
  verified, and replaces the app on restart — no trip to the website for an installer
- The sidebar says when a new version is out; opening it lists the notes of **every
  release** between the one running and the new one
- The first start after an update says "Updated to vX" and shows what changed
- Running out of GitHub's query allowance now says how many minutes until it is back,
  instead of "403 Forbidden"

### Settings

- Every option is the same row: name and hint on the left, the control on the right,
  a hairline between rows
- The sync page no longer repeats its own title; on/off options are all switches
- The sidebar's action list uses the same row, dragged to reorder, switched to hide

### Fixes

- Sync no longer claims "the vault is a subdirectory of the repository" when the
  vault sits behind a junction or an 8.3 short path
- The installer carries `LICENSE` and `NOTICE.md`: MIT and ISC both ask for the
  notices to travel with the distribution

## 0.2.0

The first release: a Markdown vault drawn natively in Go with MyGo, one executable,
no WebView. Documents, PDF, images, Word, whiteboards, wiki links and a knowledge
graph; sync to a folder, a GitHub repository or WebDAV; the interface switches
between Chinese and English.
