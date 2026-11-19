# discordbot
Testing out discord bot integration

## Playlist tags

Post a Spotify track link in the songs channel and the bot adds it to a playlist. Each **tag** points to one playlist; add one after the link to choose where the song goes:

```
https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8 !relax
```

| Message | Result |
| --- | --- |
| `<link>` | Default playlist |
| `<link> you gotta hear this!` | Default playlist |
| `<link> !relax` | `relax`'s playlist only, **not** the default |
| `<link> +relax` | `relax`'s playlist **and** the default playlist |
| `<link> !relax !gym` | Both tags' playlists |
| `<link> !default` | Default playlist (same as no tag) |
| `<link> !typo` | Default playlist (unknown tags are ignored) |

- **`!` means only, `+` means also the default.** `+relax` is a shortcut for `!relax !default`.
- **Matching is forgiving:** tags are case-insensitive and can go anywhere in the message. They also work inside formatting, e.g. `**!relax**` or `(+relax)`.
- **No duplicates:** each playlist gets a song at most once, even when several tags point to the same playlist. A song that's already in a playlist isn't added again.
- **Every link counts:** all links in the message go to the selected playlists.
- **Links only:** tags in a message without a Spotify link do nothing.
- **Permissions:** songs are added with the poster's own Spotify account, so every tagged playlist must be collaborative, or otherwise editable by everyone who posts.

## Managing tags

Tags are managed in Discord with slash commands, and changes apply immediately:

| Command | What it does |
| --- | --- |
| `/tags` | Lists the playlists and the tags for each one. Only you see the reply. |
| `/tag add tag:relax playlist:<link>` | Creates `!relax` (and `+relax`) for the playlist. If the tag already exists, nothing changes. |
| `/tag set tag:relax playlist:<link>` | Changes which playlist `!relax` adds to, creating the tag if it doesn't exist. |
| `/tag remove tag:relax` | Deletes the tag. Songs tagged with it go to the default playlist. |

- **Playlist links:** in Spotify, use **Share → Copy link to playlist**. The playlist must be collaborative so everyone's songs can be added. Spotify-made playlists (IDs starting with `37i9dQZF1`) are rejected because nobody can add to them.
- **Tag names:** letters and numbers, optionally joined by single `-` or `_` (`lo-fi`, `road_trip`). They're case-insensitive, and the `!` or `+` is optional when typing them in a command.
- **`default` is reserved:** it always means the playlist in `SPOTIFY_PLAYLIST_ID`.
- **Aliases:** several tags can point to the same playlist (e.g. `!relax` and `!relaxing`). `/tags` lists them together.
- **Changes are public:** `/tag` replies in the channel, so everyone sees what changed.
- **Who can use them:** anyone in the server, by default. Server admins can limit `/tag` to certain roles under **Server Settings → Integrations → (the bot) → Commands**.

**Pick names that say what's in the playlist.** Generic names like `!both`, `!all` or `!misc` are hard to remember. Names like `!play` or `!help` collide with other bots' commands, and near-duplicates like `!relax`/`!relaxx` are easy to mix up.

### Where tags are stored

Playlist IDs aren't committed to this public repo. Tags live in `/data/playlist-tags.json` inside the `discord-bot-data` Docker volume on the server, which survives redeploys. Back it up with:

```sh
docker cp discord-bot:/data/playlist-tags.json .
```

The first time the bot starts without that file, it creates it from the `SPOTIFY_PLAYLIST_TRIGGERS` secret (format `tag=playlistID[,playlistID...];tag=playlistID`). After that, the secret is ignored and can be deleted.

Tags used to point to several playlists. Older files and secrets are converted automatically: each tag keeps its first playlist other than the default one, since `+tag` now covers the default. The bot logs a warning for each tag that changed, listing the playlists it dropped, and rewrites the file in the new format. The original file is kept as `playlist-tags.json.v1.bak`, so dropped playlists can be recovered, and it's needed to roll back to an older version of the bot: copy it back over `playlist-tags.json`, because older versions can't read the new format. If the file can't be read or written, the bot logs the error, falls back to the secret's tags, and `/tag` is unavailable until it's fixed. The bot logs `Loaded playlist triggers` with the tag names at startup.

### Other settings

| Variable | Default | Description |
| --- | --- | --- |
| `SPOTIFY_TRIGGER_PREFIX` | `!` | The single symbol that marks a tag. It can't be `+`, which marks tags that also add to the default playlist. Avoid `$` (Docker Compose treats it as a variable) and `#`/`:` (hashtags and `:emoji:` would look like tags). |
| `SPOTIFY_REPLY_UNKNOWN_TRIGGERS` | `false` | When `true`, the bot replies `Unknown playlist: !name` when someone uses a tag that isn't configured. |
| `SPOTIFY_TAGS_FILE` | `playlist-tags.json` | Where tags are saved. `docker-compose.yml` sets it to `/data/playlist-tags.json` on the volume. |
