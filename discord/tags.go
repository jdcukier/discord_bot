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
	tagAliasSubcommand  = "alias"

	tagOption      = "tag"
	playlistOption = "playlist"
	newTagsOption  = "new"
)

// Limits on /tag alias, to keep its reply short
const (
	maxAliases       = 10
	maxNewTagsLength = 300
)

// Discord's limits on the length of a message, and of an autocomplete choice's name and value
const (
	maxMessageLength = 2000
	maxChoiceLength  = 100
)

// TagStore reads and changes the playlist tags
type TagStore interface {
	Config() *trigger.Config
	Add(name, playlistID string) (existing string, err error)
	Set(name, playlistID string) (previous string, err error)
	Remove(name string) (previous string, err error)
	AddAliases(existing string, names []string) (trigger.AliasResult, error)
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
			MaxLength:    trigger.MaxNameLength,
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
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        tagAliasSubcommand,
					Description: "Add more tags to an existing tag's playlist",
					Options: []*discordgo.ApplicationCommandOption{
						tagName("Existing tag whose playlist the new tags add to, or default for the default playlist", true),
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        newTagsOption,
							Description: fmt.Sprintf("Up to %d new tag names, separated by spaces or commas, e.g. lifting workout", maxAliases),
							Required:    true,
							MaxLength:   maxNewTagsLength,
						},
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

	playlistID string // Playlist the command's tag adds to, if it succeeded, so its name can be looked up
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
	byPlaylist := tagsByPlaylist(c)
	names := map[string]string{}
	for id := range byPlaylist {
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

	line := func(id string) string { return playlistLine(c, id, names[id], byPlaylist[id], nil) }

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
	blocks := []string{howTo, "\n**Playlists**\n" + line(c.DefaultPlaylistID)}
	for _, id := range others {
		blocks = append(blocks, line(id))
	}
	if len(c.Tags) == 0 {
		blocks = append(blocks, "No other tags yet.")
	}
	blocks = append(blocks, strings.Join([]string{
		"\n> **Manage**",
		"> `/tag add` — create a tag",
		"> `/tag set` — create a tag or change its playlist",
		"> `/tag remove` — delete a tag",
		"> `/tag alias` — add more tags to a tag's playlist",
	}, "\n"))
	return chunkLines(blocks, maxMessageLength)
}

// tagsByPlaylist groups the tags by playlist, each sorted. The default playlist always has its reserved tag.
func tagsByPlaylist(c *trigger.Config) map[string][]string {
	byPlaylist := map[string][]string{c.DefaultPlaylistID: {trigger.DefaultName}}
	for tag, id := range c.Tags {
		byPlaylist[id] = append(byPlaylist[id], tag)
	}
	for _, tags := range byPlaylist {
		slices.Sort(tags)
	}
	return byPlaylist
}

// playlistLine formats a playlist and its tags as in /tags, bolding the tags listed in bold.
// name is the playlist's name, or "" if it isn't known. A playlist without tags says so.
func playlistLine(c *trigger.Config, id, name string, tags, bold []string) string {
	return playlistLineWithout(c, id, name, tags, bold, nil)
}

// playlistLineWithout is playlistLine with the tags in omitted left out, each run of them shown as an ellipsis
func playlistLineWithout(c *trigger.Config, id, name string, tags, bold []string, omitted map[string]bool) string {
	var formatted []string
	for i, tag := range tags {
		switch {
		case !omitted[tag]:
			item := "`" + tag + "`"
			if slices.Contains(bold, tag) {
				item = "**" + item + "**"
			}
			formatted = append(formatted, item)
		case i == 0 || !omitted[tags[i-1]]:
			formatted = append(formatted, "…")
		}
	}
	list := strings.Join(formatted, ", ")
	if len(tags) == 0 {
		list = "no tags left"
	}
	return fmt.Sprintf("🎧 **%s** — %s", playlistMarkdownLink(c, id, name), list)
}

// playlistMarkdownLink formats a playlist as a masked link without an embed preview, labelled with
// its name (see cleanPlaylistName), or with the /tags fallback label if the name isn't known
func playlistMarkdownLink(c *trigger.Config, id, name string) string {
	return fmt.Sprintf("[%s](<%s>)", playlistLabel(c, id, name), playlistURL(id))
}

// playlistLabel formats a playlist's name (see cleanPlaylistName) as escaped text,
// or as the /tags fallback label if the name isn't known
func playlistLabel(c *trigger.Config, id, name string) string {
	name = cleanPlaylistName(name)
	if name == "" {
		name = untitledPlaylistLabel
		if id == c.DefaultPlaylistID {
			name = defaultPlaylistLabel
		}
	}
	return escapeMarkdown(name)
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

// subtextSeparator separates the parts of a tag reply's subtext
const subtextSeparator = " · "

// minPlaylistLineLength is the room a tag reply's playlist line keeps, if it needs it, before the subtext
// is shortened. It fits the longest link (see maxPlaylistNameLength) with a few tags.
const minPlaylistLineLength = 600

// tagChangeReply formats the public reply to a tag change: the header line, the playlist's line as
// in /tags (bolding the tags listed in bold), and the non-empty subtext parts on one subtext line.
// c must be the configuration after the change. name is the playlist's raw name, or "" if it isn't
// known; it's cleaned and escaped like in /tags. Tags in bold that aren't on the playlist are ignored.
//
// header and the subtext parts are inserted as raw, trusted markdown: callers must escape anything
// users or Spotify control (e.g. with playlistLabel or escapeMarkdown), and keep each one on a single
// line, since a newline breaks the layout.
//
// The reply fits in a message. If it's too long, the last subtext parts are left out first, while the
// playlist line needs the room (up to minPlaylistLineLength, or more to keep the link and bolded tags),
// then the playlist line's tags are left out, bolded ones last.
func tagChangeReply(c *trigger.Config, header, playlistID, name string, bold []string, subtext ...string) string {
	subtext = slices.DeleteFunc(slices.Clone(subtext), func(part string) bool { return part == "" })
	tags := tagsByPlaylist(c)[playlistID]
	lines := []string{header}
	if len(subtext) > 0 {
		// Keep room for the link with the bolded tags, at least, so the subtext is cut before them
		unbolded := map[string]bool{}
		for _, tag := range tags {
			unbolded[tag] = !slices.Contains(bold, tag)
		}
		boldOnly := playlistLineWithout(c, playlistID, name, tags, bold, unbolded)
		lineRoom := min(len(playlistLine(c, playlistID, name, tags, bold)), max(minPlaylistLineLength, len(boldOnly)))
		room := maxMessageLength - len(header) - len("\n") - lineRoom - len("\n")
		if sub := joinFitting("-# ", subtext, subtextSeparator, room); sub != "" {
			lines = append(lines, sub)
		}
	}
	budget := maxMessageLength - len(strings.Join(lines, "\n")) - len("\n")
	line := fitPlaylistLine(c, playlistID, name, tags, bold, budget)
	lines = slices.Insert(lines, 1, line)
	return truncate(strings.Join(lines, "\n"), maxMessageLength)
}

// joinFitting joins the parts with sep after prefix in at most limit bytes, leaving out the last
// parts (marked with an ellipsis part) if needed. It returns "" if not even one part fits whole.
func joinFitting(prefix string, parts []string, sep string, limit int) string {
	for n := len(parts); n > 0; n-- {
		kept := slices.Clone(parts[:n])
		if n < len(parts) {
			kept = append(kept, "…")
		}
		if s := prefix + strings.Join(kept, sep); len(s) <= limit {
			return s
		}
	}
	return ""
}

// fitPlaylistLine formats a playlist line of at most limit bytes, leaving out tags from the end
// (unbolded ones first) if needed, so the link and the remaining tags stay whole. Without room
// for the link, it returns a bare ellipsis line, or "" if even that doesn't fit.
func fitPlaylistLine(c *trigger.Config, id, name string, tags, bold []string, limit int) string {
	omitted := map[string]bool{}
	for {
		line := playlistLineWithout(c, id, name, tags, bold, omitted)
		if len(line) <= limit {
			return line
		}
		if len(omitted) == len(tags) {
			break
		}
		next := ""
		for _, tag := range slices.Backward(tags) {
			if !omitted[tag] && (next == "" || (slices.Contains(bold, next) && !slices.Contains(bold, tag))) {
				next = tag
			}
		}
		omitted[next] = true
	}
	const fallback = "🎧 …"
	if len(fallback) <= limit {
		return fallback
	}
	return ""
}

// cachedPlaylistNames returns the store's cached playlist name lookup. It returns "" for unknown names.
func cachedPlaylistNames(store TagStore) func(id string) string {
	names := playlistNames(store)
	return func(id string) string {
		if names == nil || id == "" {
			return ""
		}
		return names(id)
	}
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
	// The old playlist's name is read first, since it's forgotten if the change leaves it without tags
	names := cachedPlaylistNames(store)
	oldID := c.Tags[name]
	oldName := names(oldID)
	previous, err := change(name, playlistID)
	if err != nil {
		return tagReply{content: failure + err.Error(), private: true}
	}
	if previous == playlistID {
		return tagReply{content: fmt.Sprintf("`%s%s` already adds to that playlist.", c.Prefix, name), private: true, playlistID: playlistID}
	}
	if previous != "" && !replace {
		return tagReply{content: fmt.Sprintf("`%s%s` already adds to %s. Use `/tag set` to change its playlist.",
			c.Prefix, name, playlistName(c, previous)), private: true}
	}
	if previous != oldID { // Changed by someone else meanwhile
		oldName = names(previous)
	}

	header := fmt.Sprintf("✅ **Added `%s%s`**", c.Prefix, name)
	var subtext []string
	if previous != "" {
		header = fmt.Sprintf("🔁 **Moved `%s%s`**", c.Prefix, name)
		subtext = append(subtext, "Was: "+playlistMarkdownLink(c, previous, oldName))
	}
	if playlistID != c.DefaultPlaylistID { // On the default playlist, both forms add to it alone
		subtext = append(subtext, fmt.Sprintf("`%s%s` also adds to the default", trigger.PlusPrefix, name))
	}
	if playlistID != c.DefaultPlaylistID && !slices.Contains(slices.Collect(maps.Values(c.Tags)), playlistID) {
		subtext = append(subtext, "Playlist must be collaborative")
	}
	content := tagChangeReply(store.Config(), header, playlistID, names(playlistID), []string{name}, subtext...)
	return tagReply{content: content, playlistID: playlistID}
}

// removeTag handles /tag remove
func removeTag(store TagStore, rawName string) tagReply {
	c := store.Config()
	name := normalizeTagName(c, rawName)
	// The playlist's name is read first, since it's forgotten if the playlist is left without tags
	names := cachedPlaylistNames(store)
	oldID := c.Tags[name]
	oldName := names(oldID)
	previous, err := store.Remove(name)
	if err != nil {
		return tagReply{content: "Couldn't remove the tag: " + err.Error(), private: true}
	}
	if previous != oldID { // Changed by someone else meanwhile
		oldName = names(previous)
	}
	header := fmt.Sprintf("🗑️ **Deleted `%s%s`**", c.Prefix, name)
	subtext := fmt.Sprintf("Songs tagged `%s%s` now go to the default playlist", c.Prefix, name)
	return tagReply{content: tagChangeReply(store.Config(), header, previous, oldName, nil, subtext)}
}

// aliasTag handles /tag alias, which adds tags to an existing tag's playlist, or to the default
// playlist for default. rawNames lists the new tags, separated by spaces or commas. Tags that
// already exist are never moved, so nobody replaces one by accident.
func aliasTag(store TagStore, rawExisting, rawNames string) tagReply {
	c := store.Config()
	existing := normalizeTagName(c, rawExisting)
	const failure = "Couldn't add the tags: "
	var names []string
	for _, raw := range strings.FieldsFunc(rawNames, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if name := normalizeTagName(c, raw); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return tagReply{content: failure + "list at least one new tag name.", private: true}
	}
	if len(names) > maxAliases {
		return tagReply{content: fmt.Sprintf("%sadd at most %d at once.", failure, maxAliases), private: true}
	}

	result, err := store.AddAliases(existing, names)
	if err != nil {
		return tagReply{content: failure + err.Error(), private: true}
	}
	c = store.Config()
	nameOf := cachedPlaylistNames(store)
	skipped := skippedAliases(c, result.Skipped, nameOf)
	if len(result.Added) == 0 {
		const sep = "\n- "
		header := fmt.Sprintf("No tags added to `%s%s`'s playlist:", c.Prefix, existing)
		content := cmp.Or(joinFitting(header+sep, skipped, sep, maxMessageLength), header+sep+"…")
		return tagReply{content: content, private: true}
	}
	header := fmt.Sprintf("✅ **Added aliases for `%s%s`**", c.Prefix, existing)
	content := tagChangeReply(c, header, result.PlaylistID, nameOf(result.PlaylistID), result.Added, skipped...)
	return tagReply{content: content, playlistID: result.PlaylistID}
}

// skippedAliases explains why /tag alias left names out, with one part per reason.
// name returns a playlist's name, or "" if it isn't known.
func skippedAliases(c *trigger.Config, skipped []trigger.SkippedAlias, name func(id string) string) []string {
	type group struct {
		reason     trigger.AliasSkip
		tooLong    bool // An invalid name that's too long, which deserves its own reason
		playlistID string
	}
	var groups []group
	names := map[group][]string{}
	for _, skip := range skipped {
		tooLong := skip.Reason == trigger.AliasInvalid && utf8.RuneCountInString(skip.Name) > trigger.MaxNameLength
		g := group{skip.Reason, tooLong, skip.PlaylistID}
		if _, ok := names[g]; !ok {
			groups = append(groups, g)
		}
		// Different invalid names can look the same once cleaned up, e.g. as (blank)
		if shown := skippedAliasName(skip.Name); !slices.Contains(names[g], shown) {
			names[g] = append(names[g], shown)
		}
	}
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		one := len(names[g]) == 1
		var reason string
		switch g.reason {
		case trigger.AliasExists:
			reason = plural(one, "already an alias", "already aliases")
		case trigger.AliasTaken:
			reason = fmt.Sprintf("already on %s, use /tag set to move %s",
				playlistLabel(c, g.playlistID, name(g.playlistID)), plural(one, "it", "them"))
		case trigger.AliasInvalid:
			reason = plural(one, "invalid name", "invalid names")
			if g.tooLong {
				reason = fmt.Sprintf("longer than %d characters", trigger.MaxNameLength)
			}
		case trigger.AliasReserved:
			reason = "reserved for the default playlist"
		}
		parts = append(parts, fmt.Sprintf("Skipped %s (%s)", strings.Join(names[g], ", "), reason))
	}
	return parts
}

// skippedAliasName formats a name /tag alias left out as code. Invalid names are shown as typed,
// so backticks, which would end the code span, are dropped, and names too long to be tags are cut.
func skippedAliasName(name string) string {
	name = strings.ReplaceAll(name, "`", "")
	if name == "" {
		return "(blank)"
	}
	if runes := []rune(name); len(runes) > trigger.MaxNameLength {
		name = string(runes[:trigger.MaxNameLength]) + "…"
	}
	return "`" + name + "`"
}

// plural returns singular if one is true, and plural otherwise
func plural(one bool, singular, many string) string {
	if one {
		return singular
	}
	return many
}

// autocompleteTags suggests existing tag names that contain what the user has typed,
// along with the reserved default tag if withDefault is true
func autocompleteTags(c *trigger.Config, typed string, withDefault bool) []*discordgo.ApplicationCommandOptionChoice {
	typed = normalizeTagName(c, typed)
	names := slices.Collect(maps.Keys(c.Tags))
	if withDefault {
		names = append(names, trigger.DefaultName)
	}
	slices.Sort(names)
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, name := range names {
		// Discord rejects choices over 100 characters. Tag names are shorter, but this keeps
		// one bad tag from breaking autocomplete for everyone.
		if !strings.Contains(name, typed) || utf8.RuneCountInString(c.Prefix+name) > maxChoiceLength {
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

// truncate shortens s to at most limit bytes, marking the cut with an ellipsis if there's room for it
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const ellipsis = "…"
	if limit < len(ellipsis) {
		return s[:runeBoundary(s, max(limit, 0))]
	}
	return s[:runeBoundary(s, limit-len(ellipsis))] + ellipsis
}

// runeBoundary returns the largest index <= n that doesn't split a UTF-8 character
func runeBoundary(s string, n int) int {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}
