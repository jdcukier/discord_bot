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
- **Tag names:**
  - Write them lowercase and without the `!` prefix.
  - Use letters and numbers, optionally joined by a single `-` or `_` (pattern `[\p{L}\p{N}]+(?:[_-][\p{L}\p{N}]+)*`). Names can't start or end with `-`/`_`, and can't contain `--` or `__`.
  - Spaces, emoji, and other punctuation aren't allowed. Suggest a valid alternative instead.
- **`default` is reserved.** Never write `default=...`, because it makes the whole value invalid. `!default` already means `SPOTIFY_PLAYLIST_ID`.
  - If a tag should add to the default playlist plus others, list the default playlist's ID explicitly, e.g. `both=<DEFAULT_ID>,<OTHER_ID>`.
- **One entry per tag.** When several tags share a playlist, repeat the ID in each tag's entry. When one tag adds to several playlists, list them comma-separated. Don't list the same ID twice within a tag.
- **Keep the order:** keep existing entries in their original order, update changed ones in place, and add new ones at the end in the order the user listed them. Within a tag, put the default playlist's ID first, then the others in the order given. Order has no effect on behavior; this just keeps output predictable.

## Output

Reply with:

1. **The value** in its own code block, ready to paste.
2. **A table** with one row per tag (`!tag`) and the playlist(s) it adds to, using playlist names when the user gave them. Include `!default` as a built-in row.
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
- Every name matches the name pattern, and every ID is a playlist ID.
- The new value still contains every existing tag the user didn't ask to remove.
