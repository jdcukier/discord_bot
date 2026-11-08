---
name: playlist-tags
description: Build the value of the SPOTIFY_PLAYLIST_TRIGGERS secret for this Discord bot from Spotify playlist links and the tags (e.g. "!relax") that should add to them. Use when the user wants to add, change, rename, or remove playlist tags, or asks what to paste into SPOTIFY_PLAYLIST_TRIGGERS.
---

# Playlist tags → `SPOTIFY_PLAYLIST_TRIGGERS`

Turn "these links, these tags" into the single line the user pastes into the `SPOTIFY_PLAYLIST_TRIGGERS` GitHub repository secret. The format and the bot's behavior are documented in `README.md` under "Playlist tags" and "Configuring tags". The parser is `spotify/trigger/trigger.go`, and it is the source of truth if this skill and the code ever disagree.

## Inputs to collect

1. **The current secret value.** GitHub secrets can't be read back, and the new value replaces the old one completely. If the user is adding to existing tags, ask them to paste the current value. If they say there isn't one, or they want to start over, build from scratch and say that existing tags will be dropped.
2. **Playlist links or IDs**, each with the tags that should add to it.
3. **The default playlist ID (`SPOTIFY_PLAYLIST_ID`)**, only if a tag should also add to the default playlist. Ask for it; don't guess.

The input may be a casual message, such as a Discord announcement listing tags and linked playlist names. Pull the tag-to-playlist mapping out of it.

- **Ambiguity:** if part of the request is ambiguous, give your best-guess value anyway. State the assumption in one line and offer the alternative. Only stop and ask when you can't produce a sensible value at all, for example when a link is missing.
- **`!default` with a link:** if the input links a playlist next to `!default`, check that its ID matches the `SPOTIFY_PLAYLIST_ID` the user gave. Don't write an entry for it. If the IDs differ, warn the user: `!default` always uses `SPOTIFY_PLAYLIST_ID`, so they need to update that secret too.

## Rules

Each rule mirrors `ParsePlaylists` and `New` in `spotify/trigger/trigger.go`.

- **Format:** `tag=id1,id2;tag2=id3`. Entries are separated by `;` and IDs within an entry by `,`. Output it as one line with no spaces, quotes, or trailing `;`.
- **Playlist ID:** take the part after `/playlist/` and before `?` or the end of the link. For example, `https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc` gives `37i9dQZF1DXcBWIGoYBM5M`.
  - IDs are normally 22 letters and digits. Flag anything that doesn't look like that.
  - Reject track (`/track/`), album, and artist links: they aren't playlists.
  - Spotify-owned editorial playlists (IDs starting with `37i9dQZF1`) can never be added to: posters can't edit them. Warn before building the value and suggest copying the songs into a collaborative playlist the user owns. Include the ID only if the user confirms anyway.
- **Tag names:**
  - Write them lowercase and without the `!` prefix.
  - Use letters and numbers, optionally joined by a single `-` or `_` (pattern `[\p{L}\p{N}]+(?:[_-][\p{L}\p{N}]+)*`). Names can't start or end with `-`/`_`, and can't contain `--` or `__`.
  - Spaces, emoji, and other punctuation aren't allowed. Suggest a valid alternative instead.
- **`default` is reserved.** Never write `default=...`, because it makes the whole value invalid. `!default` already means `SPOTIFY_PLAYLIST_ID`.
  - If a tag should add to the default playlist plus others, list the default playlist's ID explicitly, e.g. `relaxtoo=<DEFAULT_ID>,<RELAX_ID>`.
- **One entry per tag.** When several tags share a playlist, repeat the ID in each tag's entry. When one tag adds to several playlists, list them comma-separated. Don't list the same ID twice within a tag.
- **Keep the order:** keep existing entries in their original order, update changed ones in place, and add new ones at the end in the order the user listed them. Within a tag, put the default playlist's ID first, then the others in the order given. Order has no effect on behavior; this just keeps output predictable.

## Push back on unclear tag names

A tag name that's valid can still be a bad choice. Before building the value, check every **new or renamed** tag. Don't re-check existing tags the user isn't changing. Flag a name if it:

- **Describes how many playlists, not what's in them:** `both`, `all`, `every`, `everything`, `multi`, `combo`. These stop making sense once another playlist is added.
- **Is generic or vague:** `misc`, `other`, `stuff`, `music`, `songs`, `playlist`, `new`, `good`, `fav`, `main`, `def`. People can't tell which playlist they mean.
- **Looks like a bot command:** `play`, `skip`, `stop`, `help`, `queue`, `p`, `np`, `search`. These collide with other bots' commands and with ordinary chat, so they trigger by accident.
- **Is easily confused with another tag:** a one-letter difference from another tag that goes to *different* playlists (e.g. `relax` vs `relaxx`), or a name that suggests a different playlist than the one it points to.
- **Is a single letter or hard to type**, e.g. it needs `_`/`-` combinations that are awkward on a phone keyboard.

Deliberate aliases are fine and shouldn't be flagged: several spellings that all point to the *same* playlists (`relax`, `relaxing`, `rehlax`).

When a name is flagged:

1. **Warn before producing the value.** Give the reason in one line, and suggest one or two clearer names that describe the content, e.g. `relaxtoo` or `chillplus` instead of `both`. Base the suggestions on the playlist names the user gave. If you don't know what a playlist contains, ask instead of guessing.
2. **Wait for the user to choose.** Don't silently swap in your suggestion.
3. **If they confirm the original name,** use it as written without further argument. It's their server.

If some names are fine and others are flagged, ask about the flagged ones and build the full value only after the user answers.

## Output

Reply with:

1. **The value** in its own code block, ready to paste.
2. **A table** with one row per tag (`!tag`) and the playlist(s) it adds to. Use playlist names when the user gave them, otherwise the IDs; don't name playlists from memory. Include `!default` as a built-in row.
3. **Notes, only where they apply:**
   - Tags that also include the default playlist's ID must be updated if `SPOTIFY_PLAYLIST_ID` changes.
   - Every tagged playlist must be collaborative, or editable by everyone who posts.
   - Any existing tags that were removed or changed.
   - Any way the user's announcement or description no longer matches the config, so they can update it before posting.
4. **How to apply:**
   - Update the secret under **Settings → Secrets and variables → Actions**.
   - Re-run only the **`release-to-prod / deploy`** job of the latest main workflow run. Not "Re-run all jobs", which creates a new release.
   - Check the bot logs for `Loaded playlist triggers`.

Don't write the real value into any file in this repo, since the repo is public. Put it only in your reply.

## Self-check before answering

- No `default=` entry, no empty entries, and no spaces.
- Every new or renamed tag passed the naming check, or the user confirmed it after a warning.
- Every name matches the name pattern, and every ID is a playlist ID.
- The new value still contains every existing tag the user didn't ask to remove.
