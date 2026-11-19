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

## Managing tags

Tags are managed in Discord with slash commands, and changes apply immediately:

| Command | What it does |
| --- | --- |
| `/tags` | Lists every tag and its playlists. Only you see the reply. |
| `/tag add tag:gym playlist:<link>` | Makes `!gym` add songs to the playlist, creating the tag if it's new. Run it again with another link to make one tag add to several playlists. |
| `/tag remove tag:gym playlist:<link>` | Stops `!gym` adding to that playlist. The tag is deleted when its last playlist is removed. |
| `/tag remove tag:gym` | Deletes `!gym`. Songs tagged with it go to the default playlist. |

- **Playlist links:** in Spotify, use **Share → Copy link to playlist**. The playlist must be collaborative so everyone's songs can be added. Spotify-made playlists (IDs starting with `37i9dQZF1`) are rejected because nobody can add to them.
- **Tag names:** letters and numbers, optionally joined by single `-` or `_` (`lo-fi`, `road_trip`). They're case-insensitive, and the `!` is optional when typing them in a command.
- **`default` is reserved:** it always means the playlist in `SPOTIFY_PLAYLIST_ID`. For a tag that also adds to the default playlist, add the default playlist's link to it.
- **Sharing:** one tag can add to several playlists, and several tags can add to the same playlist (e.g. `!relax` and `!relaxing` as aliases).
- **Changes are public:** `/tag` replies in the channel, so everyone sees what changed.
- **Who can use them:** anyone in the server, by default. Server admins can limit `/tag` to certain roles under **Server Settings → Integrations → (the bot) → Commands**.

**Pick names that say what the tag does.** Generic names like `!both`, `!all` or `!misc` stop making sense once more playlists exist. Names like `!play` or `!help` collide with other bots' commands, and near-duplicates like `!relax`/`!relaxx` are easy to mix up.

### Where tags are stored

Playlist IDs aren't committed to this public repo. Tags live in `/data/playlist-tags.json` inside the `discord-bot-data` Docker volume on the server, which survives redeploys. Back it up with:

```sh
docker cp discord-bot:/data/playlist-tags.json .
```

The first time the bot starts without that file, it creates it from the `SPOTIFY_PLAYLIST_TRIGGERS` secret (format `tag=playlistID[,playlistID...];tag=playlistID`). After that, the secret is ignored and can be deleted. If the file can't be read or written, the bot logs the error, falls back to the secret's tags, and `/tag` is unavailable until it's fixed. The bot logs `Loaded playlist triggers` with the tag names at startup.

### Other settings

| Variable | Default | Description |
| --- | --- | --- |
| `SPOTIFY_TRIGGER_PREFIX` | `!` | The single symbol that marks a tag. Avoid `$` (Docker Compose treats it as a variable) and `#`/`:` (hashtags and `:emoji:` would look like tags). |
| `SPOTIFY_REPLY_UNKNOWN_TRIGGERS` | `false` | When `true`, the bot replies `Unknown playlist: !name` when someone uses a tag that isn't configured. |
| `SPOTIFY_TAGS_FILE` | `playlist-tags.json` | Where tags are saved. `docker-compose.yml` sets it to `/data/playlist-tags.json` on the volume. |
