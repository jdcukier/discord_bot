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
	tagSetSubcommand    = "set"
	tagRemoveSubcommand = "remove"

	tagOption      = "tag"
	playlistOption = "playlist"
)

// maxMessageLength is Discord's limit on the length of a message
const maxMessageLength = 2000

// TagStore reads and changes the playlist tags
type TagStore interface {
	Config() *trigger.Config
	Add(name, playlistID string) (existing string, err error)
	Set(name, playlistID string) (previous string, err error)
	Remove(name string) (previous string, err error)
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
			Description: "Change which playlist a tag adds songs to",
			Contexts:    guildOnly,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagAddSubcommand,
					Description: "Create a new tag that adds songs to a playlist",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Tag name, e.g. gym (letters and numbers, optionally joined by - or _)", false),
						playlistLinkOption(),
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagSetSubcommand,
					Description: "Make a tag add songs to a playlist, creating the tag or replacing its playlist",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Tag name, e.g. gym (letters and numbers, optionally joined by - or _)", true),
						playlistLinkOption(),
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagRemoveSubcommand,
					Description: "Delete a tag",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Tag to delete", true),
					},
				},
			},
		},
	}
}

// playlistLinkOption is the required playlist link option
func playlistLinkOption() *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        playlistOption,
		Description: "Spotify playlist link. It must be collaborative so everyone's songs can be added.",
		Required:    true,
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

// listTags formats the tags, grouped by playlist, as messages of at most maxMessageLength characters
func listTags(c *trigger.Config) []string {
	// Group the tags by playlist, with the default playlist first
	byPlaylist := map[string][]string{c.DefaultPlaylistID: {trigger.DefaultName}}
	for _, name := range slices.Sorted(maps.Keys(c.Tags)) {
		id := c.Tags[name]
		byPlaylist[id] = append(byPlaylist[id], name)
	}
	others := slices.DeleteFunc(slices.Collect(maps.Keys(byPlaylist)), func(id string) bool { return id == c.DefaultPlaylistID })
	slices.SortFunc(others, func(a, b string) int { return strings.Compare(byPlaylist[a][0], byPlaylist[b][0]) })

	formatTags := func(names []string) string {
		tags := make([]string, len(names))
		for i, name := range names {
			tags[i] = "`" + c.Prefix + name + "`"
		}
		return strings.Join(tags, ", ")
	}

	lines := []string{
		fmt.Sprintf("**Playlist tags**: put one after a song link. `%sname` adds the song to that playlist only, "+
			"`%sname` also adds it to the default playlist. Without a tag, songs go to the default playlist.",
			c.Prefix, trigger.PlusPrefix),
		fmt.Sprintf("**Default playlist** %s: %s", playlistLink(c.DefaultPlaylistID), formatTags(byPlaylist[c.DefaultPlaylistID])),
	}
	for _, id := range others {
		lines = append(lines, fmt.Sprintf("%s: %s", playlistLink(id), formatTags(byPlaylist[id])))
	}
	if len(c.Tags) == 0 {
		lines = append(lines, "No other tags yet. Create one with `/tag add`.")
	}
	return chunkLines(lines, maxMessageLength)
}

// addTag handles /tag add, which only creates new tags so nobody replaces one by accident
func addTag(store TagStore, rawName, rawPlaylist string) tagReply {
	return changeTag(store, rawName, rawPlaylist, false)
}

// setTag handles /tag set, which creates a tag or replaces its playlist
func setTag(store TagStore, rawName, rawPlaylist string) tagReply {
	return changeTag(store, rawName, rawPlaylist, true)
}

// changeTag creates a tag, or replaces its playlist if replace is true
func changeTag(store TagStore, rawName, rawPlaylist string, replace bool) tagReply {
	c := store.Config()
	name := normalizeTagName(c, rawName)
	failure := "Couldn't add the tag: "
	if replace {
		failure = "Couldn't set the tag: "
	}
	playlistID, err := trigger.ParsePlaylistID(rawPlaylist)
	if err != nil {
		return tagReply{content: failure + err.Error(), private: true}
	}
	change := store.Add
	if replace {
		change = store.Set
	}
	previous, err := change(name, playlistID)
	if err != nil {
		return tagReply{content: failure + err.Error(), private: true}
	}
	if previous == playlistID {
		return tagReply{content: fmt.Sprintf("`%s%s` already adds to that playlist.", c.Prefix, name), private: true}
	}
	if previous != "" && !replace {
		return tagReply{content: fmt.Sprintf("`%s%s` already adds to %s. Use `/tag set` to change its playlist.",
			c.Prefix, name, playlistName(c, previous)), private: true}
	}
	msg := fmt.Sprintf("`%s%s` now adds songs to %s, and `%s%s` adds them to the default playlist too.",
		c.Prefix, name, playlistName(c, playlistID), trigger.PlusPrefix, name)
	if previous != "" {
		msg += fmt.Sprintf(" It used to add to %s.", playlistName(c, previous))
	}
	if playlistID != c.DefaultPlaylistID && !slices.Contains(slices.Collect(maps.Values(c.Tags)), playlistID) {
		msg += "\nThe playlist must be collaborative, or everyone else's songs can't be added to it."
	}
	return tagReply{content: msg}
}

// removeTag handles /tag remove
func removeTag(store TagStore, rawName string) tagReply {
	c := store.Config()
	name := normalizeTagName(c, rawName)
	if _, err := store.Remove(name); err != nil {
		return tagReply{content: "Couldn't remove the tag: " + err.Error(), private: true}
	}
	return tagReply{content: fmt.Sprintf("Deleted `%s%s`. Songs tagged with it now go to the default playlist.", c.Prefix, name)}
}

// autocompleteTags suggests existing tag names that contain what the user has typed
func autocompleteTags(c *trigger.Config, typed string) []*discordgo.ApplicationCommandOptionChoice {
	typed = normalizeTagName(c, typed)
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, name := range slices.Sorted(maps.Keys(c.Tags)) {
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
	if after, ok := strings.CutPrefix(name, c.Prefix); ok {
		name = after
	} else {
		name = strings.TrimPrefix(name, trigger.PlusPrefix)
	}
	return strings.ToLower(name)
}

// playlistName formats a playlist for a reply
func playlistName(c *trigger.Config, id string) string {
	if id == c.DefaultPlaylistID {
		return "the default playlist"
	}
	return playlistLink(id)
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
