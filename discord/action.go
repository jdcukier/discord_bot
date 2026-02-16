package discord

import "context"

type Action interface {
	Execute(ctx context.Context)
}

type Actions []Action

// ChannelActions is a map of channel IDs to action names
type ChannelActions map[string][]string

// Add an action to the map of actions for a channel
func (a ChannelActions) Add(channelID string, action string) {
	if a == nil {
		a = make(ChannelActions)
	}
	if _, ok := a[channelID]; !ok {
		a[channelID] = []string{}
	}
	a[channelID] = append(a[channelID], action)
}
