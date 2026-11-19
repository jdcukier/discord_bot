package discord

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
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

// PlaylistNamer looks up playlist names. It returns "" for playlists whose name isn't known.
// /tags calls it once per playlist before replying, and Discord drops interactions that aren't
// answered within 3 seconds, so PlaylistName must only read cached names and must not call Spotify.
type PlaylistNamer interface {
	PlaylistName(id string) string
}

// playlistNames returns the tag store's playlist name lookup, or nil if it doesn't know names
func playlistNames(tags TagStore) func(id string) string {
	if names, ok := tags.(PlaylistNamer); ok {
		return names.PlaylistName
	}
	return nil
}

// Labels for playlists whose name isn't known
const (
	untitledPlaylistLabel = "Untitled playlist"
	defaultPlaylistLabel  = "Default playlist"
)

// maxPlaylistNameLength is the most characters of a playlist name /tags shows, so a long name
// can't push its line over maxMessageLength and get split inside the link
const maxPlaylistNameLength = 100

// exampleTags are used in the /tags examples when there aren't enough real tags.
// There are more than two, so there's always one that's different from the first example.
var exampleTags = []string{"gym", "chill", "party"}

// listTags formats the playlists with their tags as messages of at most maxMessageLength characters.
// name returns a playlist's name, or "" if it isn't known.
func listTags(c *trigger.Config, name func(id string) string) []string {
	// Group the tags by playlist. The default playlist always has its reserved tag.
	byPlaylist := map[string][]string{c.DefaultPlaylistID: {trigger.DefaultName}}
	for tag, id := range c.Tags {
		byPlaylist[id] = append(byPlaylist[id], tag)
	}
	names := map[string]string{}
	for id, tags := range byPlaylist {
		slices.Sort(tags)
		if name != nil {
			names[id] = cleanPlaylistName(name(id))
		}
	}

	// The default playlist comes first, then the others by name. Unknown names go last, by ID.
	others := slices.DeleteFunc(slices.Collect(maps.Keys(byPlaylist)), func(id string) bool { return id == c.DefaultPlaylistID })
	slices.SortFunc(others, func(a, b string) int {
		na, nb := names[a], names[b]
		if (na == "") != (nb == "") {
			if na == "" {
				return 1
			}
			return -1
		}
		return cmp.Or(
			strings.Compare(strings.ToLower(na), strings.ToLower(nb)),
			strings.Compare(na, nb),
			strings.Compare(a, b),
		)
	})

	playlistLine := func(id string) string {
		label := names[id]
		if label == "" {
			label = untitledPlaylistLabel
			if id == c.DefaultPlaylistID {
				label = defaultPlaylistLabel
			}
		}
		tags := make([]string, len(byPlaylist[id]))
		for i, tag := range byPlaylist[id] {
			tags[i] = "`" + tag + "`"
		}
		return fmt.Sprintf("🎧 **[%s](<%s>)** — %s", escapeMarkdown(label), playlistURL(id), strings.Join(tags, ", "))
	}

	first, second := exampleTagNames(c)
	howTo := strings.Join([]string{
		"## 🎵 Playlist tags",
		"> **How to tag**",
		fmt.Sprintf("> `<song link> %s%s` — add to that playlist only", c.Prefix, first),
		fmt.Sprintf("> `<song link> %s%s` — add to that playlist **and** the default", trigger.PlusPrefix, first),
		fmt.Sprintf("> `<song link> %s%s %s%s` — add to several", c.Prefix, first, c.Prefix, second),
		"> No tag → default playlist",
	}, "\n")

	// Each block stays in one message unless it's too long on its own.
	// A leading newline leaves a blank line between sections.
	blocks := []string{howTo, "\n**Playlists**\n" + playlistLine(c.DefaultPlaylistID)}
	for _, id := range others {
		blocks = append(blocks, playlistLine(id))
	}
	if len(c.Tags) == 0 {
		blocks = append(blocks, "No other tags yet.")
	}
	blocks = append(blocks, strings.Join([]string{
		"\n> **Manage**",
		"> `/tag add` — create a tag",
		"> `/tag set` — create a tag or change its playlist",
		"> `/tag remove` — delete a tag",
	}, "\n"))
	return chunkLines(blocks, maxMessageLength)
}

// exampleTagNames picks two different tags for the /tags examples, preferring, in order:
// real tags on two different playlists other than the default one, names from exampleTags
// that aren't tags (so they can't mislead), any other real tag, and any name from exampleTags.
// It never returns "".
func exampleTagNames(c *trigger.Config) (first, second string) {
	tags := slices.Sorted(maps.Keys(c.Tags))
	var unused []string // Example names that aren't tags
	for _, tag := range exampleTags {
		if _, exists := c.Tags[tag]; !exists {
			unused = append(unused, tag)
		}
	}
	// pick returns the first name in the lists that isn't first and that ok accepts
	pick := func(ok func(tag string) bool, lists ...[]string) string {
		for _, list := range lists {
			for _, tag := range list {
				if tag != first && ok(tag) {
					return tag
				}
			}
		}
		return ""
	}
	notDefault := func(tag string) bool { return c.Tags[tag] != c.DefaultPlaylistID }
	anyTag := func(string) bool { return true }

	first = cmp.Or(pick(notDefault, tags), pick(anyTag, unused, tags, exampleTags))
	// Only a real first tag has a playlist that a second tag can differ from
	differentPlaylist := func(string) bool { return false }
	if firstID, isTag := c.Tags[first]; isTag {
		differentPlaylist = func(tag string) bool { return c.Tags[tag] != firstID }
	}
	second = cmp.Or(
		pick(func(tag string) bool { return differentPlaylist(tag) && notDefault(tag) }, tags),
		pick(differentPlaylist, tags), // Tags on the default playlist work too
		pick(anyTag, unused, tags, exampleTags),
	)
	return first, second
}

// Characters that join or modify the character before them
const (
	zeroWidthJoiner    = 0x200D
	zeroWidthNonJoiner = 0x200C
)

// hiddenCharacters are invisible characters dropped from playlist names: the zero-width space,
// bidi marks (including the Arabic letter mark), embeddings, overrides and isolates, and the byte
// order mark. Other format characters are kept, since joiners and emoji tag characters are part
// of words, emoji and flags. Ranges must stay in ascending order.
var hiddenCharacters = &unicode.RangeTable{R16: []unicode.Range16{
	{Lo: 0x061C, Hi: 0x061C, Stride: 1},
	{Lo: 0x200B, Hi: 0x200B, Stride: 1},
	{Lo: 0x200E, Hi: 0x200F, Stride: 1},
	{Lo: 0x202A, Hi: 0x202E, Stride: 1},
	{Lo: 0x2066, Hi: 0x2069, Stride: 1},
	{Lo: 0xFEFF, Hi: 0xFEFF, Stride: 1},
}}

// cleanPlaylistName makes a playlist name safe to show on one line: control characters and line
// or paragraph separators become spaces, hiddenCharacters are dropped, runs of spaces are
// collapsed, and names over maxPlaylistNameLength characters are cut with an ellipsis.
// It doesn't escape markdown; see escapeMarkdown.
func cleanPlaylistName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		case unicode.Is(hiddenCharacters, r):
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) <= maxPlaylistNameLength {
		return name
	}
	runes := []rune(name)
	cut := maxPlaylistNameLength - 1 // Leaves room for the ellipsis
	// Don't separate a character from what modifies or joins it, e.g. an accent or an emoji sequence
	for cut > 0 && (extendsPrevious(runes[cut]) || runes[cut-1] == zeroWidthJoiner) {
		cut--
	}
	// Flags are pairs of regional indicators, so don't cut after an odd number of them
	if cut > 0 && isRegionalIndicator(runes[cut]) {
		run := 0
		for i := cut - 1; i >= 0 && isRegionalIndicator(runes[i]); i-- {
			run++
		}
		if run%2 == 1 {
			cut--
		}
	}
	if cut == 0 { // Nothing to cut cleanly at, e.g. a letter with many accents
		cut = maxPlaylistNameLength - 1
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}

// isRegionalIndicator reports whether r is one of the letters that make up flag emoji
func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// extendsPrevious reports whether r belongs with the character before it: a combining mark,
// variation selector, joiner, emoji tag character or emoji skin tone modifier
func extendsPrevious(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		r == zeroWidthJoiner || r == zeroWidthNonJoiner ||
		(r >= 0xE0020 && r <= 0xE007F) ||
		(r >= 0x1F3FB && r <= 0x1F3FF)
}

// markdownEscaper escapes the characters that would break a masked link or format its text
var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, "[", `\[`, "]", `\]`, "*", `\*`, "_", `\_`, "`", "\\`", "~", `\~`, "|", `\|`, ">", `\>`,
)

// escapeMarkdown escapes s so Discord shows it as plain text, e.g. as a link's text
func escapeMarkdown(s string) string {
	return markdownEscaper.Replace(s)
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
	return "<" + playlistURL(id) + ">"
}

// playlistURL is a playlist's Spotify link
func playlistURL(id string) string {
	return "https://open.spotify.com/playlist/" + id
}

// chunkLines joins lines into messages no longer than limit (in bytes, which is never
// fewer than Discord's character count). Lines are only split if they're too long on their own.
// A "line" may contain newlines, to keep a block in one message. Leading newlines are dropped
// at the start of a message.
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
		} else {
			line = strings.TrimLeft(line, "\n")
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
