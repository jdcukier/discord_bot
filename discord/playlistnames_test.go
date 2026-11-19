package discord

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"discordbot/spotify/trigger"
	"discordbot/spotify/worker"
)

// fakeNameFetcher returns names from a map, or err
type fakeNameFetcher struct {
	mu       sync.Mutex
	names    map[string]string
	err      error
	userErrs map[string]error // Errors for some users, e.g. ones who haven't connected Spotify
	untilCtx bool             // If set, every call blocks until its context is done, and fails
	calls    []string         // Playlist IDs looked up
	users    []string         // Users each lookup was made for

	wait map[int]chan struct{} // Calls (counted from 0) that block until their channel is closed
	hold chan struct{}         // If set, every call blocks until it's closed

	active, maxActive int             // Calls in progress, and the most at once
	deadlines         []time.Duration // How long each call had until its deadline, or 0 without one
}

func (f *fakeNameFetcher) FetchPlaylistName(ctx context.Context, userID, playlistID string) (string, error) {
	f.mu.Lock()
	call := len(f.calls)
	f.calls = append(f.calls, playlistID)
	f.users = append(f.users, userID)
	var left time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	f.deadlines = append(f.deadlines, left)
	f.active++
	f.maxActive = max(f.maxActive, f.active)
	name, err := f.names[playlistID], cmp.Or(f.userErrs[userID], f.err)
	wait, hold := f.wait[call], f.hold
	f.mu.Unlock()
	if wait != nil {
		<-wait
	}
	if hold != nil {
		<-hold
	}
	if f.untilCtx {
		<-ctx.Done()
		name, err = "", ctx.Err()
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return name, err
}

// lookups returns the playlist IDs looked up so far
func (f *fakeNameFetcher) lookups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func TestPlaylistNamesName(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", defaultTestID: "Everything", chillTestID: "Chill"}}
	names := NewPlaylistNames(fetcher, store)

	// A missing name is looked up with the user's Spotify access, within a short timeout, and saved
	assert.Equal(t, "Gym", names.Name(context.Background(), "user-id", gymTestID))
	assert.Equal(t, []string{gymTestID}, fetcher.calls)
	assert.Equal(t, []string{"user-id"}, fetcher.users)
	assert.Positive(t, fetcher.deadlines[0])
	assert.LessOrEqual(t, fetcher.deadlines[0], playlistNameLookupTimeout)
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))

	// A saved name is returned without a lookup
	fetcher.names[gymTestID] = "Renamed"
	assert.Equal(t, "Gym", names.Name(context.Background(), "user-id", gymTestID))
	assert.Len(t, fetcher.calls, 1)

	// The default playlist's name is saved too, even though no tag adds to it
	assert.Equal(t, "Everything", names.Name(context.Background(), "user-id", defaultTestID))
	assert.Equal(t, "Everything", store.PlaylistName(defaultTestID))

	// Other playlists' names are returned but not saved
	assert.Equal(t, "Chill", names.Name(context.Background(), "user-id", chillTestID))
	assert.Empty(t, store.PlaylistName(chillTestID))

	// Nothing to look up without an ID
	calls := len(fetcher.calls)
	assert.Empty(t, names.Name(context.Background(), "user-id", ""))
	assert.Len(t, fetcher.calls, calls)
}

func TestPlaylistNamesNameFails(t *testing.T) {
	// Without Spotify access, or on errors, the name stays unknown and nothing is saved.
	// The fetcher never starts the auth flow, and the lookup posts nothing to Discord (there's no session).
	for _, err := range []error{worker.ErrAuthRequired, errors.New("spotify is down")} {
		store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
		fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}, err: err}
		names := NewPlaylistNames(fetcher, store)
		assert.Empty(t, names.Name(context.Background(), "user-id", gymTestID), err)
		assert.Equal(t, []string{gymTestID}, fetcher.calls, err)
		assert.Empty(t, store.PlaylistName(gymTestID), err)

		// A saved name is kept
		require.NoError(t, store.SetPlaylistName(defaultTestID, "Everything"))
		assert.Equal(t, map[string]string{defaultTestID: "Everything"},
			names.Names(context.Background(), "user-id", []string{defaultTestID, gymTestID}), err)
	}
}

func TestPlaylistNamesNameKeepsNameSavedMeanwhile(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	release := make(chan struct{})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}, wait: map[int]chan struct{}{0: release}}
	names := NewPlaylistNames(fetcher, store)

	done := make(chan string)
	go func() { done <- names.Name(context.Background(), "user-id", gymTestID) }()
	require.Eventually(t, func() bool { return len(fetcher.lookups()) == 1 }, time.Second, time.Millisecond)
	require.NoError(t, store.SetPlaylistName(gymTestID, "Saved meanwhile"))
	close(release)
	assert.Equal(t, "Gym", <-done)
	assert.Equal(t, "Saved meanwhile", store.PlaylistName(gymTestID), "names are only saved if none is set")
}

func TestPlaylistNamesNames(t *testing.T) {
	ids := []string{defaultTestID}
	tags := map[string][]string{}
	all := map[string]string{defaultTestID: "Everything"}
	for i := range 10 {
		id := fmt.Sprintf("%022d", i)
		ids = append(ids, id)
		tags[fmt.Sprintf("tag%d", i)] = []string{id}
		all[id] = fmt.Sprintf("Playlist %d", i)
	}
	store := newTestTagStore(t, tags)
	require.NoError(t, store.SetPlaylistName(ids[1], "Saved"))
	hold := make(chan struct{})
	fetcher := &fakeNameFetcher{names: all, hold: hold}
	names := NewPlaylistNames(fetcher, store)

	done := make(chan map[string]string)
	go func() { done <- names.Names(context.Background(), "user-id", append(ids, ids[2])) }()
	// The missing names are looked up in parallel, a few at a time
	require.Eventually(t, func() bool { return len(fetcher.lookups()) == maxPlaylistNameLookups }, time.Second, time.Millisecond)
	assert.Never(t, func() bool { return len(fetcher.lookups()) > maxPlaylistNameLookups }, 20*time.Millisecond, time.Millisecond)
	close(hold)
	got := <-done

	want := maps.Clone(all)
	want[ids[1]] = "Saved"
	assert.Equal(t, want, got)
	assert.ElementsMatch(t, slices.Delete(slices.Clone(ids), 1, 2), fetcher.lookups(), "saved and repeated names aren't looked up")
	assert.Equal(t, maxPlaylistNameLookups, fetcher.maxActive)
	for _, id := range ids {
		assert.Equal(t, want[id], store.PlaylistName(id))
	}
}

func TestPlaylistNamesLastLookupWins(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	release := make(chan struct{})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Old"}, wait: map[int]chan struct{}{0: release}}
	names := NewPlaylistNames(fetcher, store)

	// The first lookup (a refresh) gets the old name but finishes last
	done := make(chan struct{})
	go func() {
		names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
		close(done)
	}()
	require.Eventually(t, func() bool { return len(fetcher.lookups()) == 1 }, time.Second, time.Millisecond)
	fetcher.mu.Lock()
	fetcher.names[gymTestID] = "New"
	fetcher.mu.Unlock()
	assert.Equal(t, "New", names.Name(context.Background(), "user-id", gymTestID))
	assert.Equal(t, "New", store.PlaylistName(gymTestID))

	close(release)
	<-done
	assert.Equal(t, "New", store.PlaylistName(gymTestID))
}

func TestPlaylistNamesFailedLookupKeepsEarlierResult(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	release := make(chan struct{})
	fetcher := &fakeNameFetcher{
		names:    map[string]string{gymTestID: "Gym"},
		userErrs: map[string]error{"no-spotify": worker.ErrAuthRequired},
		wait:     map[int]chan struct{}{0: release},
	}
	names := NewPlaylistNames(fetcher, store)

	// A refresh with Spotify access is in flight when someone without it runs /tags
	done := make(chan struct{})
	go func() {
		names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
		close(done)
	}()
	require.Eventually(t, func() bool { return len(fetcher.lookups()) == 1 }, time.Second, time.Millisecond)
	assert.Empty(t, names.Name(context.Background(), "no-spotify", gymTestID))

	// The later lookup failed, so the earlier one is still saved
	close(release)
	<-done
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))
}

func TestPlaylistNamesFailedLookupsDontHoldOffRefreshes(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}, userErrs: map[string]error{"no-spotify": worker.ErrAuthRequired}}
	names := NewPlaylistNames(fetcher, store)

	// Neither a failed lookup for a reply nor a failed refresh counts as the hourly refresh
	assert.Empty(t, names.Name(context.Background(), "no-spotify", gymTestID))
	names.RefreshStale(context.Background(), "no-spotify", []string{gymTestID})
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Equal(t, []string{gymTestID, gymTestID, gymTestID}, fetcher.lookups())
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))

	// A successful one does
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Len(t, fetcher.lookups(), 3)
}

func TestPlaylistNamesNamesSkipsQueuedLookupsAfterTimeout(t *testing.T) {
	ids := make([]string, 10)
	tags := map[string][]string{}
	for i := range ids {
		ids[i] = fmt.Sprintf("%022d", i)
		tags[fmt.Sprintf("tag%d", i)] = []string{ids[i]}
	}
	store := newTestTagStore(t, tags)
	fetcher := &fakeNameFetcher{untilCtx: true}
	names := NewPlaylistNames(fetcher, store)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.Empty(t, names.Names(ctx, "user-id", ids))
	// Only the lookups that had started ran; the queued ones were skipped
	assert.Len(t, fetcher.lookups(), maxPlaylistNameLookups)
	names.mu.Lock()
	assert.Len(t, names.lookups, maxPlaylistNameLookups)
	assert.Empty(t, names.refreshed)
	names.mu.Unlock()
}

// countingNameStore counts the calls that save names
type countingNameStore struct {
	*trigger.Store
	mu    sync.Mutex
	saves []map[string]string
}

func (s *countingNameStore) SetPlaylistNames(names map[string]string) error {
	s.mu.Lock()
	s.saves = append(s.saves, maps.Clone(names))
	s.mu.Unlock()
	return s.Store.SetPlaylistNames(names)
}

func TestPlaylistNamesNamesSavesOnce(t *testing.T) {
	store := &countingNameStore{Store: newTestTagStore(t, map[string][]string{"gym": {gymTestID}, "chill": {chillTestID}})}
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", chillTestID: "Chill", defaultTestID: "Everything"}}
	names := NewPlaylistNames(fetcher, store)

	ids := []string{defaultTestID, gymTestID, chillTestID}
	names.Names(context.Background(), "user-id", ids)
	assert.Equal(t, []map[string]string{{gymTestID: "Gym", chillTestID: "Chill", defaultTestID: "Everything"}}, store.saves)
	for _, id := range ids {
		assert.Equal(t, fetcher.names[id], store.PlaylistName(id))
	}
}

func TestPlaylistNamesRefreshStale(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	require.NoError(t, store.SetPlaylistName(gymTestID, "Gym"))
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym Bangers", defaultTestID: "Everything"}}
	names := NewPlaylistNames(fetcher, store)

	// Tagged playlists and the default one are looked up at most once an hour, replacing renamed ones
	names.RefreshStale(context.Background(), "user-id", []string{defaultTestID, gymTestID, chillTestID})
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Equal(t, []string{defaultTestID, gymTestID}, fetcher.calls)
	assert.Equal(t, "Gym Bangers", store.PlaylistName(gymTestID))
	assert.Equal(t, "Everything", store.PlaylistName(defaultTestID))
	assert.Equal(t, playlistNameTimeout, fetcher.deadlines[0].Round(time.Second))

	// A failed refresh keeps the saved name
	names.mu.Lock()
	names.refreshed = map[string]time.Time{}
	names.mu.Unlock()
	fetcher.err = worker.ErrAuthRequired
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Equal(t, "Gym Bangers", store.PlaylistName(gymTestID))

	// A name looked up for a reply counts as a refresh
	store2 := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher2 := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}}
	names2 := NewPlaylistNames(fetcher2, store2)
	names2.Name(context.Background(), "user-id", gymTestID)
	names2.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Len(t, fetcher2.calls, 1)
}

func TestPlaylistNamesNil(t *testing.T) {
	assert.Nil(t, NewPlaylistNames(nil, newTestTagStore(t, nil)))
	assert.Nil(t, NewPlaylistNames(&fakeNameFetcher{}, nil))

	var names *PlaylistNames
	names.RefreshStale(context.Background(), "user-id", []string{gymTestID})
	assert.Empty(t, names.Saved(gymTestID))
	assert.Empty(t, names.Name(context.Background(), "user-id", gymTestID))
	assert.Empty(t, names.Names(context.Background(), "user-id", []string{gymTestID}))
}

// executeAddTracksWithNames adds a song tagged +gym, with addErr as the adder's result
func executeAddTracksWithNames(t *testing.T, addErr error) (*fakeNameFetcher, *trigger.Store) {
	t.Helper()
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", defaultTestID: "Everything"}}
	action := &AddTracksToPlaylist{
		session: &discordgo.Session{},
		event: &discordgo.MessageCreate{Message: &discordgo.Message{
			Content: testLink + " +gym",
			Author:  &discordgo.User{ID: "user-id"},
		}},
		playlistAdder: &fakePlaylistAdder{err: addErr},
		triggers:      store.Config(),
		names:         NewPlaylistNames(fetcher, store),
	}
	action.Execute(context.Background())
	return fetcher, store
}

func TestAddTracksRefreshesPlaylistNames(t *testing.T) {
	// In the background, so it doesn't hold up the message handler
	fetcher, store := executeAddTracksWithNames(t, nil)
	assert.Eventually(t, func() bool {
		return store.PlaylistName(gymTestID) == "Gym" && store.PlaylistName(defaultTestID) == "Everything"
	}, time.Second, time.Millisecond)
	assert.ElementsMatch(t, []string{gymTestID, defaultTestID}, fetcher.lookups())
}

func TestAddTracksSkipsPlaylistNamesUnlessAdded(t *testing.T) {
	// Queued until the user connects Spotify, or failed: a lookup would fail too, or use up the hourly one
	for _, err := range []error{worker.ErrAuthPending, errors.New("spotify is down")} {
		fetcher, _ := executeAddTracksWithNames(t, err)
		assert.Never(t, func() bool { return len(fetcher.lookups()) > 0 }, 50*time.Millisecond, time.Millisecond, err)
	}
}

// fakeResponder records the responses to an interaction, in order
type fakeResponder struct {
	calls []string // "respond", "defer", "edit" and "followup"

	responses []*discordgo.InteractionResponse
	edits     []string
	followups []*discordgo.WebhookParams
	fetcher   *fakeNameFetcher // If set, "respond", "defer" and "edit" record how many lookups preceded them
	lookups   []int

	editErr, followupErr error // Returned by every edit or follow-up
}

func (f *fakeResponder) record(call string) {
	f.calls = append(f.calls, call)
	if f.fetcher != nil {
		f.lookups = append(f.lookups, len(f.fetcher.lookups()))
	}
}

func (f *fakeResponder) InteractionRespond(_ *discordgo.Interaction, resp *discordgo.InteractionResponse, _ ...discordgo.RequestOption) error {
	if resp.Type == discordgo.InteractionResponseDeferredChannelMessageWithSource {
		f.record("defer")
	} else {
		f.record("respond")
	}
	f.responses = append(f.responses, resp)
	return nil
}

func (f *fakeResponder) InteractionResponseEdit(_ *discordgo.Interaction, edit *discordgo.WebhookEdit, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.record("edit")
	f.edits = append(f.edits, *edit.Content)
	return &discordgo.Message{}, f.editErr
}

func (f *fakeResponder) FollowupMessageCreate(_ *discordgo.Interaction, _ bool, params *discordgo.WebhookParams, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.record("followup")
	f.followups = append(f.followups, params)
	return &discordgo.Message{}, f.followupErr
}

// testInteraction builds a slash command interaction run by user-id, with string options
func testInteraction(name, sub string, options ...string) *discordgo.InteractionCreate {
	data := discordgo.ApplicationCommandInteractionData{Name: name}
	if sub != "" {
		subOption := &discordgo.ApplicationCommandInteractionDataOption{Name: sub, Type: discordgo.ApplicationCommandOptionSubCommand}
		for i := 0; i+1 < len(options); i += 2 {
			subOption.Options = append(subOption.Options, &discordgo.ApplicationCommandInteractionDataOption{
				Name: options[i], Type: discordgo.ApplicationCommandOptionString, Value: options[i+1],
			})
		}
		data.Options = []*discordgo.ApplicationCommandInteractionDataOption{subOption}
	}
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type:   discordgo.InteractionApplicationCommand,
		Data:   data,
		Member: &discordgo.Member{User: &discordgo.User{ID: "user-id"}},
	}}
}

// testTagHandler returns a tag command handler with a name fetcher
func testTagHandler(store *trigger.Store, fetcher *fakeNameFetcher) *InteractionSessionHandler {
	return NewInteractionSessionHandler(WithTagCommands("app-id", store), WithTagPlaylistNames(NewPlaylistNames(fetcher, store)))
}

func TestTagCommandLooksUpName(t *testing.T) {
	store := newTestTagStore(t, nil)
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym *Bangers*"}}
	h := testTagHandler(store, fetcher)
	responder := &fakeResponder{fetcher: fetcher}

	h.tagCommand(responder, testInteraction(tagCommand, tagAddSubcommand, tagOption, "gym", playlistOption, gymTestID))
	// The public reply is deferred before the lookup, then filled in with the name, which is saved
	assert.Equal(t, []string{"defer", "edit"}, responder.calls)
	assert.Equal(t, []int{0, 1}, responder.lookups)
	assert.Zero(t, responder.responses[0].Data.Flags, "the reply is public")
	assert.Equal(t, "✅ **Added `!gym`**\n"+
		"🎧 **[Gym \\*Bangers\\*]"+testPlaylistTarget(gymTestID)+"** — **`gym`**\n"+
		"-# `+gym` also adds to the default · Playlist must be collaborative", responder.edits[0])
	assert.Equal(t, []string{"user-id"}, fetcher.users)
	assert.Equal(t, "Gym *Bangers*", store.PlaylistName(gymTestID))

	// Once the name is saved, the reply is sent right away
	responder = &fakeResponder{}
	h.tagCommand(responder, testInteraction(tagCommand, tagSetSubcommand, tagOption, "workout", playlistOption, gymTestID))
	assert.Equal(t, []string{"respond"}, responder.calls)
	assert.Zero(t, responder.responses[0].Data.Flags)
	assert.Contains(t, responder.responses[0].Data.Content, "🎧 **[Gym \\*Bangers\\*]")
	assert.Len(t, fetcher.calls, 1)
}

func TestTagCommandWithoutSpotifyAccess(t *testing.T) {
	store := newTestTagStore(t, nil)
	fetcher := &fakeNameFetcher{err: worker.ErrAuthRequired}
	h := testTagHandler(store, fetcher)
	responder := &fakeResponder{}

	// The tag is still added, and the reply falls back to the generic label
	h.tagCommand(responder, testInteraction(tagCommand, tagSetSubcommand, tagOption, "gym", playlistOption, gymTestID))
	assert.Equal(t, []string{"defer", "edit"}, responder.calls)
	assert.Contains(t, responder.edits[0], "🎧 **[Untitled playlist]"+testPlaylistTarget(gymTestID)+"** — **`gym`**")
	assert.Equal(t, map[string]string{"gym": gymTestID}, store.Config().Tags)
	assert.Empty(t, store.PlaylistName(gymTestID))
}

func TestTagCommandPrivateRepliesSkipLookups(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}})
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", chillTestID: "Chill"}}
	h := testTagHandler(store, fetcher)

	// Errors and "already" replies are private, sent right away without deferring or looking anything up
	for _, i := range []*discordgo.InteractionCreate{
		testInteraction(tagCommand, tagAddSubcommand, tagOption, "gym", playlistOption, chillTestID),
		testInteraction(tagCommand, tagAddSubcommand, tagOption, "gym", playlistOption, gymTestID),
		testInteraction(tagCommand, tagSetSubcommand, tagOption, "chill", playlistOption, "not a link"),
		testInteraction(tagCommand, tagAliasSubcommand, tagOption, "nope", newTagsOption, "x"),
		testInteraction(tagCommand, tagAliasSubcommand, tagOption, "gym", newTagsOption, "gym"),
		testInteraction(tagCommand, tagRemoveSubcommand, tagOption, "nope"),
	} {
		responder := &fakeResponder{}
		h.tagCommand(responder, i)
		assert.Equal(t, []string{"respond"}, responder.calls)
		assert.Equal(t, discordgo.MessageFlagsEphemeral, responder.responses[0].Data.Flags, responder.responses[0].Data.Content)
	}

	// Removing a tag replies right away with the saved name
	responder := &fakeResponder{}
	h.tagCommand(responder, testInteraction(tagCommand, tagRemoveSubcommand, tagOption, "gym"))
	assert.Equal(t, []string{"respond"}, responder.calls)
	assert.Zero(t, responder.responses[0].Data.Flags)
	assert.Empty(t, fetcher.calls)
}

func TestTagAliasCommandLooksUpDefaultName(t *testing.T) {
	store := newTestTagStore(t, nil)
	fetcher := &fakeNameFetcher{names: map[string]string{defaultTestID: "Everything"}}
	h := testTagHandler(store, fetcher)
	responder := &fakeResponder{}

	h.tagCommand(responder, testInteraction(tagCommand, tagAliasSubcommand, tagOption, "default", newTagsOption, "main"))
	assert.Equal(t, []string{"defer", "edit"}, responder.calls)
	assert.Zero(t, responder.responses[0].Data.Flags)
	assert.Equal(t, "✅ **Added aliases for `!default`**\n"+
		"🎧 **[Everything]"+testPlaylistTarget(defaultTestID)+"** — `default`, **`main`**", responder.edits[0])
	assert.Equal(t, "Everything", store.PlaylistName(defaultTestID))
}

func TestTagsCommandLooksUpNames(t *testing.T) {
	store := newTestTagStore(t, map[string][]string{"gym": {gymTestID}, "chill": {chillTestID}})
	require.NoError(t, store.SetPlaylistName(chillTestID, "Chill"))
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym", defaultTestID: "Everything"}}
	h := testTagHandler(store, fetcher)
	responder := &fakeResponder{fetcher: fetcher}

	// The private reply is deferred, then the missing names (including the default playlist's) are
	// looked up with the user's Spotify access and saved
	h.tagsCommand(responder, testInteraction(tagsCommand, ""))
	assert.Equal(t, []string{"defer", "edit"}, responder.calls)
	assert.Equal(t, []int{0, 2}, responder.lookups)
	assert.Equal(t, discordgo.MessageFlagsEphemeral, responder.responses[0].Data.Flags)
	assert.ElementsMatch(t, []string{gymTestID, defaultTestID}, fetcher.calls)
	assert.Equal(t, []string{"user-id", "user-id"}, fetcher.users)
	assert.Equal(t, listTags(store.Config(), store.PlaylistName), responder.edits)
	assert.Contains(t, responder.edits[0], "**Playlists**\n🎧 **[Everything]"+testPlaylistTarget(defaultTestID)+"** — `default`\n"+
		"🎧 **[Chill]"+testPlaylistTarget(chillTestID)+"** — `chill`\n🎧 **[Gym]"+testPlaylistTarget(gymTestID)+"** — `gym`\n")
	assert.Equal(t, "Everything", store.PlaylistName(defaultTestID))
	assert.Equal(t, "Gym", store.PlaylistName(gymTestID))

	// Once every name is saved, the reply is sent right away
	responder = &fakeResponder{}
	h.tagsCommand(responder, testInteraction(tagsCommand, ""))
	assert.Equal(t, []string{"respond"}, responder.calls)
	assert.Equal(t, discordgo.MessageFlagsEphemeral, responder.responses[0].Data.Flags)
	assert.Len(t, fetcher.calls, 2)
}

func TestTagsCommandSplitsLongLists(t *testing.T) {
	tags := map[string][]string{}
	for i := range 40 {
		tags[fmt.Sprintf("a-very-long-tag-name-for-testing-%d", i)] = []string{fmt.Sprintf("%022d", i)}
	}
	store := newTestTagStore(t, tags)
	h := testTagHandler(store, &fakeNameFetcher{err: worker.ErrAuthRequired})
	responder := &fakeResponder{}

	h.tagsCommand(responder, testInteraction(tagsCommand, ""))
	chunks := listTags(store.Config(), nil)
	require.Greater(t, len(chunks), 1)
	// The first message fills in the deferred reply, and the others are private follow-ups
	assert.Equal(t, append([]string{"defer", "edit"}, slices.Repeat([]string{"followup"}, len(chunks)-1)...), responder.calls)
	assert.Equal(t, chunks[:1], responder.edits)
	for i, followup := range responder.followups {
		assert.Equal(t, chunks[i+1], followup.Content)
		assert.Equal(t, discordgo.MessageFlagsEphemeral, followup.Flags)
	}
	assert.True(t, strings.HasPrefix(responder.edits[0], "## 🎵 Playlist tags\n**Playlists**\n"))
}

func TestDeferredReplyFallsBackToFollowup(t *testing.T) {
	store := newTestTagStore(t, nil)
	fetcher := &fakeNameFetcher{names: map[string]string{gymTestID: "Gym"}}
	h := testTagHandler(store, fetcher)

	// The public reply is sent as a public follow-up when the deferred reply can't be filled in
	responder := &fakeResponder{editErr: errors.New("unknown webhook")}
	h.tagCommand(responder, testInteraction(tagCommand, tagAddSubcommand, tagOption, "gym", playlistOption, gymTestID))
	assert.Equal(t, []string{"defer", "edit", "followup"}, responder.calls)
	require.Len(t, responder.followups, 1)
	assert.Equal(t, responder.edits[0], responder.followups[0].Content)
	assert.Contains(t, responder.followups[0].Content, "🎧 **[Gym]")
	assert.Zero(t, responder.followups[0].Flags)
	assert.Equal(t, &discordgo.MessageAllowedMentions{}, responder.followups[0].AllowedMentions)

	// /tags does the same privately, then sends the rest
	tags := map[string][]string{}
	for i := range 40 {
		tags[fmt.Sprintf("a-very-long-tag-name-for-testing-%d", i)] = []string{fmt.Sprintf("%022d", i)}
	}
	store = newTestTagStore(t, tags)
	h = testTagHandler(store, &fakeNameFetcher{err: worker.ErrAuthRequired})
	responder = &fakeResponder{editErr: errors.New("unknown webhook")}
	h.tagsCommand(responder, testInteraction(tagsCommand, ""))
	chunks := listTags(store.Config(), nil)
	require.Greater(t, len(chunks), 1)
	assert.Equal(t, append([]string{"defer", "edit"}, slices.Repeat([]string{"followup"}, len(chunks))...), responder.calls)
	for i, followup := range responder.followups {
		assert.Equal(t, chunks[i], followup.Content)
		assert.Equal(t, discordgo.MessageFlagsEphemeral, followup.Flags)
	}

	// If the follow-up fails too, nothing else is sent
	responder = &fakeResponder{editErr: errors.New("unknown webhook"), followupErr: errors.New("unknown webhook")}
	h.tagsCommand(responder, testInteraction(tagsCommand, ""))
	assert.Equal(t, []string{"defer", "edit", "followup"}, responder.calls)
}
