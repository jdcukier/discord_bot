# discordbot
Testing out discord bot integration

## Playlist tags

Post a Spotify track link in the songs channel and the bot adds it to a playlist. Add one or more **tags** to choose which playlists it goes to:

```
https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8 !relaxing
```

| Message | Result |
| --- | --- |
| `<link>` | Default playlist |
| `<link> you gotta hear this!` | Default playlist |
| `<link> !relaxing` | The playlist(s) `relaxing` points to, **not** the default |
| `<link> !default !relaxing` | The default playlist **and** `relaxing`'s playlist(s) |
| `<link> !typo` | Default playlist (unknown tags are ignored) |

- **Tags replace the default.** To add a song to the default playlist as well, include `!default`, or use a tag that points to both (see below).
- **Matching is forgiving:** tags are case-insensitive and can go anywhere in the message. They also work inside formatting, e.g. `**!relaxing**` or `(!relaxing)`.
- **No duplicates:** each playlist gets a song at most once, even when several tags point to the same playlist. A song that's already in a playlist isn't added again.
- **Every link counts:** all links in the message go to the selected playlists.
- **Links only:** tags in a message without a Spotify link do nothing.
- **Permissions:** songs are added with the poster's own Spotify account, so every tagged playlist must be collaborative, or otherwise editable by everyone who posts.

## Configuring tags

Tags are set in the `SPOTIFY_PLAYLIST_TRIGGERS` repository secret (in `.env` when running locally). It's a single line:

```
tag=playlistID[,playlistID...];tag=playlistID
```

- **Tag names:** letters and numbers, optionally joined by single `-` or `_` (`lo-fi`, `road_trip`). They're case-insensitive and written without the `!`.
- **`default` is reserved:** it always means the playlist in `SPOTIFY_PLAYLIST_ID`. Putting `default=` in the secret makes the config invalid.
- **Sharing:** one tag can point to several playlists, and several tags can point to the same playlist. Each tag needs its own entry.
- **Invalid values don't break the bot:** if the value is invalid, the bot logs the error at startup and ignores all tags until it's fixed, so every song goes to the default playlist.

### Finding a playlist ID

In Spotify, use **Share → Copy link to playlist**. The ID is the part between `/playlist/` and `?`:

```
https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc123
                                  └──────── ID ────────┘
```

### Example

```
relax=<RELAX_ID>;relaxing=<RELAX_ID>;relaxtoo=<DEFAULT_ID>,<RELAX_ID>
```

| Tag | Adds to |
| --- | --- |
| `!relax`, `!relaxing` | The relax playlist |
| `!relaxtoo` | The default playlist and the relax playlist |
| `!default` | The default playlist (built in) |

A tag that should also add to the default playlist just lists the default playlist's ID, i.e. the value of `SPOTIFY_PLAYLIST_ID`. If the default playlist ever changes, update those tags too.

**Pick names that say what the tag does.** Generic names like `!both`, `!all` or `!misc` stop making sense once more playlists exist. Names like `!play` or `!help` collide with other bots' commands, and near-duplicates like `!relax`/`!relaxx` are easy to mix up.

### Applying changes

1. **Edit the secret:** **Settings → Secrets and variables → Actions → `SPOTIFY_PLAYLIST_TRIGGERS`**. Secrets can't be read back, so keep a copy of the full value somewhere: an update replaces the whole thing.
2. **Redeploy:** open the latest run of the **main** workflow in **Actions** and re-run only the **`release-to-prod / deploy`** job. "Re-run all jobs" would also create a new version tag and release. A run can only be re-run within 30 days; after that, merge any change to `main` to redeploy.
3. **Check it loaded:** the bot logs should show `Loaded playlist triggers` with the tag list. If they show `Invalid playlist triggers` instead, fix the value and redeploy.

### Other settings

| Variable | Default | Description |
| --- | --- | --- |
| `SPOTIFY_TRIGGER_PREFIX` | `!` | The single symbol that marks a tag. Avoid `$` (Docker Compose treats it as a variable) and `#`/`:` (hashtags and `:emoji:` would look like tags). |
| `SPOTIFY_REPLY_UNKNOWN_TRIGGERS` | `false` | When `true`, the bot replies `Unknown playlist: !name` when someone uses a tag that isn't configured. |

### Using an AI assistant

The `playlist-tags` skill in `.agents/skills/` builds the secret value for you. Give Claude Code or Codex the playlist links and the tags you want, along with the current secret value, and it returns the exact line to paste.
