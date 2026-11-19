package discord

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"

	"discordbot/constants/zapkey"
	"discordbot/spotify/trigger"
)

// Slash commands for managing playlist tags
const (
	tagsCommand = "tags" // Lists the tags
	tagCommand  = "tag"  // Changes the tags, via the subcommands below

	tagAddSubcommand    = "add"
	tagRemoveSubcommand = "remove"

	tagOption      = "tag"
	playlistOption = "playlist"
)

// maxMessageLength is Discord's limit on the length of a message
const maxMessageLength = 2000

// TagStore reads and changes the playlist tags
type TagStore interface {
	Config() *trigger.Config
	Add(name, playlistID string) (playlistIDs []string, added bool, err error)
	Remove(name, playlistID string) (remaining []string, err error)
}

// tagCommands returns the slash command definitions for managing tags.
// They're limited to servers; server admins can restrict who may use /tag
// under Server Settings → Integrations.
func tagCommands() []*discordgo.ApplicationCommand {
	guildOnly := &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	tagName := func(desc string, autocomplete bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         tagOption,
			Description:  desc,
			Required:     true,
			Autocomplete: autocomplete,
			MaxLength:    50,
		}
	}
	return []*discordgo.ApplicationCommand{
		{
			Name:        tagsCommand,
			Description: "List the playlist tags you can add after a song link",
			Contexts:    guildOnly,
		},
		{
			Name:        tagCommand,
			Description: "Change which playlists a tag adds songs to",
			Contexts:    guildOnly,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagAddSubcommand,
					Description: "Make a tag add songs to a playlist, creating the tag if it's new",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Tag name, e.g. gym (letters and numbers, optionally joined by - or _)", true),
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        playlistOption,
							Description: "Spotify playlist link. It must be collaborative so everyone's songs can be added.",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagRemoveSubcommand,
					Description: "Stop a tag adding to a playlist, or delete the tag",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Tag to change", true),
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        playlistOption,
							Description: "Playlist link to remove from the tag. Leave empty to delete the whole tag.",
						},
					},
				},
			},
		},
	}
}

// registerTagCommands creates (or updates) the tag slash commands and reports whether all succeeded.
// Creating a command with an existing name replaces it, so other commands are left alone.
func registerTagCommands(s *discordgo.Session, appID string) bool {
	ok := true
	for _, cmd := range tagCommands() {
		if _, err := s.ApplicationCommandCreate(appID, "", cmd); err != nil {
			logger.Error("Failed to register slash command", zap.String(zapkey.Command, cmd.Name), zap.Error(err))
			ok = false
			continue
		}
		logger.Info("Registered slash command", zap.String(zapkey.Command, cmd.Name))
	}
	return ok
}

// tagReply is the response to a tag command
type tagReply struct {
	content string
	private bool // Only shown to the user who ran the command
}

// listTags formats the tags as messages of at most maxMessageLength characters
func listTags(c *trigger.Config) []string {
	lines := []string{
		"**Playlist tags**: add one or more after a song link, e.g. `<link> " + c.Prefix + "gym`. Without a tag, songs go to the default playlist.",
		fmt.Sprintf("`%s%s` → %s", c.Prefix, trigger.DefaultName, playlistLink(c.DefaultPlaylistID)),
	}
	for _, name := range slices.Sorted(maps.Keys(c.Playlists)) {
		lines = append(lines, fmt.Sprintf("`%s%s` → %s", c.Prefix, name, playlistLinks(c, c.Playlists[name])))
	}
	if len(c.Playlists) == 0 {
		lines = append(lines, "No other tags yet. Create one with `/tag add`.")
	}
	return chunkLines(lines, maxMessageLength)
}

// addTag handles /tag add
func addTag(store TagStore, rawName, rawPlaylist string) tagReply {
	c := store.Config()
	name := normalizeTagName(c, rawName)
	playlistID, err := trigger.ParsePlaylistID(rawPlaylist)
	if err != nil {
		return tagReply{content: "Couldn't add the tag: " + err.Error(), private: true}
	}
	ids, added, err := store.Add(name, playlistID)
	if err != nil {
		return tagReply{content: "Couldn't add the tag: " + err.Error(), private: true}
	}
	tag := "`" + c.Prefix + name + "`"
	if !added {
		return tagReply{content: fmt.Sprintf("%s already adds to that playlist.", tag), private: true}
	}
	msg := fmt.Sprintf("%s now adds songs to %s", tag, playlistLinks(c, ids))
	if len(ids) == 1 {
		msg += "\nThe playlist must be collaborative, or everyone else's songs can't be added to it."
	}
	return tagReply{content: msg}
}

// removeTag handles /tag remove
func removeTag(store TagStore, rawName, rawPlaylist string) tagReply {
	c := store.Config()
	name := normalizeTagName(c, rawName)
	var playlistID string
	if strings.TrimSpace(rawPlaylist) != "" {
		var err error
		// Editorial playlists can't be added, but one already in a tag can still be removed
		if playlistID, err = trigger.ExtractPlaylistID(rawPlaylist); err != nil {
			return tagReply{content: "Couldn't change the tag: " + err.Error(), private: true}
		}
	}
	remaining, err := store.Remove(name, playlistID)
	if err != nil {
		return tagReply{content: "Couldn't change the tag: " + err.Error(), private: true}
	}
	tag := "`" + c.Prefix + name + "`"
	if len(remaining) == 0 {
		return tagReply{content: fmt.Sprintf("Deleted %s. Songs tagged with it now go to the default playlist.", tag)}
	}
	return tagReply{content: fmt.Sprintf("%s now adds songs to %s", tag, playlistLinks(c, remaining))}
}

// autocompleteTags suggests existing tag names that contain what the user has typed
func autocompleteTags(c *trigger.Config, typed string) []*discordgo.ApplicationCommandOptionChoice {
	typed = normalizeTagName(c, typed)
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, name := range slices.Sorted(maps.Keys(c.Playlists)) {
		if !strings.Contains(name, typed) {
			continue
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: c.Prefix + name, Value: name})
		if len(choices) == 25 { // Discord's limit
			break
		}
	}
	return choices
}

// normalizeTagName lowercases a tag name and drops a leading prefix, so "!Gym" means "gym"
func normalizeTagName(c *trigger.Config, name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, c.Prefix)
	return strings.ToLower(name)
}

// playlistLinks formats playlists as a comma-separated list
func playlistLinks(c *trigger.Config, ids []string) string {
	links := make([]string, len(ids))
	for i, id := range ids {
		if id == c.DefaultPlaylistID {
			links[i] = "the default playlist"
		} else {
			links[i] = playlistLink(id)
		}
	}
	return strings.Join(links, ", ")
}

// playlistLink formats a playlist link without an embed preview
func playlistLink(id string) string {
	return "<https://open.spotify.com/playlist/" + id + ">"
}

// chunkLines joins lines into messages no longer than limit (in bytes, which is never
// fewer than Discord's character count). Lines are only split if they're too long on their own.
func chunkLines(lines []string, limit int) []string {
	var chunks []string
	var b strings.Builder
	for _, line := range splitLongLines(lines, limit) {
		if b.Len() > 0 && b.Len()+1+len(line) > limit {
			chunks = append(chunks, b.String())
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}

// splitLongLines splits lines longer than limit, preferring to break after a comma
func splitLongLines(lines []string, limit int) []string {
	var out []string
	for _, line := range lines {
		for len(line) > limit {
			cut := strings.LastIndex(line[:limit], ", ")
			if cut <= 0 {
				cut = runeBoundary(line, limit)
			} else {
				cut += len(",")
			}
			out = append(out, line[:cut])
			line = strings.TrimLeft(line[cut:], " ")
		}
		out = append(out, line)
	}
	return out
}

// truncate shortens s to at most limit bytes, marking the cut with an ellipsis
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const ellipsis = "…"
	return s[:runeBoundary(s, limit-len(ellipsis))] + ellipsis
}

// runeBoundary returns the largest index <= n that doesn't split a UTF-8 character
func runeBoundary(s string, n int) int {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}
