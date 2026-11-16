package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/xAmarnath/gogram/internal/encoding/tl"
	"reflect"
	"strconv"
	"strings"
)

// peerResolver carries one caller's cancellation through cold lookups and packing.
type peerResolver struct {
	c       *Client
	ctx     context.Context
	request func(context.Context, tl.Object) (any, error)
	err     error
}

func (c *Client) peerResolver(ctx context.Context, request func(context.Context, tl.Object) (any, error)) *peerResolver {
	if ctx == nil {
		ctx = context.Background()
	}
	if request == nil {
		request = func(ctx context.Context, req tl.Object) (any, error) {
			if _, ok := req.(*UsersGetUsersParams); ok {
				return c.MakeRequest(ctx, req, reflect.TypeOf([]User(nil)))
			}
			return c.MakeRequest(ctx, req)
		}
	}
	return &peerResolver{c: c, ctx: ctx, request: request}
}
func (r *peerResolver) remember(err error) {
	if r.ctx.Err() != nil {
		r.err = r.ctx.Err()
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		r.err = err
	}
}
func (r *peerResolver) call(req tl.Object) (any, error) {
	if r.err != nil {
		return nil, r.err
	}
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return nil, err
	}
	value, err := r.request(r.ctx, req)
	r.remember(err)
	if r.ctx.Err() != nil {
		return nil, r.ctx.Err()
	}
	return value, err
}
func (r *peerResolver) users(ids []InputUser) ([]User, error) {
	raw, err := r.call(&UsersGetUsersParams{ID: ids})
	if err != nil {
		return nil, err
	}
	users, ok := raw.([]User)
	if !ok {
		return nil, fmt.Errorf("unexpected users response: %T", raw)
	}
	return users, nil
}
func (r *peerResolver) channels(ids []InputChannel) (MessagesChats, error) {
	raw, err := r.call(&ChannelsGetChannelsParams{ID: ids})
	if err != nil {
		return nil, err
	}
	chats, ok := raw.(MessagesChats)
	if !ok || isNilSource(chats) {
		return nil, fmt.Errorf("unexpected channels response: %T", raw)
	}
	return chats, nil
}
func (r *peerResolver) chats(ids []int64) (MessagesChats, error) {
	raw, err := r.call(&MessagesGetChatsParams{ID: ids})
	if err != nil {
		return nil, err
	}
	chats, ok := raw.(MessagesChats)
	if !ok || isNilSource(chats) {
		return nil, fmt.Errorf("unexpected chats response: %T", raw)
	}
	return chats, nil
}

// ResolvePeerContext resolves a peer without losing cancellation in cold lookups.
func (c *Client) ResolvePeerContext(ctx context.Context, peer any) (InputPeer, error) {
	return c.peerResolver(ctx, nil).resolvePeer(peer)
}
func (c *Client) ResolveUsernameContext(ctx context.Context, name string, ref ...string) (any, error) {
	return c.peerResolver(ctx, nil).resolveUsername(name, ref...)
}
func (c *Client) GetInputPeerContext(ctx context.Context, id int64) (InputPeer, error) {
	return c.peerResolver(ctx, nil).inputPeer(id)
}
func (c *Client) GetUserContext(ctx context.Context, id int64) (*UserObj, error) {
	return c.peerResolver(ctx, nil).user(id)
}
func (c *Client) GetChannelContext(ctx context.Context, id int64) (*Channel, error) {
	return c.peerResolver(ctx, nil).channel(id)
}
func (c *Client) GetChatContext(ctx context.Context, id int64) (*ChatObj, error) {
	return c.peerResolver(ctx, nil).chat(id)
}

func (r *peerResolver) user(id int64) (*UserObj, error) {
	value, err := r.c.fetchPeerOnceContext(r.ctx, cachePeerKey{'u', id}, func(context.Context) (any, error) { return r.fetchUser(id) })
	if err != nil {
		return nil, err
	}
	return value.(*UserObj), nil
}
func (r *peerResolver) channel(id int64) (*Channel, error) {
	id = trimSuffixHundred(id)
	value, err := r.c.fetchPeerOnceContext(r.ctx, cachePeerKey{'c', id}, func(context.Context) (any, error) { return r.fetchChannel(id) })
	if err != nil {
		return nil, err
	}
	return value.(*Channel), nil
}
func (r *peerResolver) chat(id int64) (*ChatObj, error) {
	if trimSuffixHundred(id) != id {
		return nil, fmt.Errorf("%d is a channel ID, not a basic group: use GetChannel", id)
	}
	if id < 0 {
		id = -id
	}
	value, err := r.c.fetchPeerOnceContext(r.ctx, cachePeerKey{'g', id}, func(context.Context) (any, error) { return r.fetchChat(id) })
	if err != nil {
		return nil, err
	}
	return value.(*ChatObj), nil
}
func (r *peerResolver) peerUser(id int64) (*InputPeerUser, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if p, err := r.c.GetPeerUser(id); err == nil {
		return p, nil
	}
	u, err := r.user(id)
	if err != nil {
		return nil, err
	}
	return &InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
}
func (r *peerResolver) peerChannel(id int64) (*InputPeerChannel, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if p, err := r.c.GetPeerChannel(id); err == nil {
		return p, nil
	}
	ch, err := r.channel(id)
	if err != nil {
		return nil, err
	}
	return &InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
}

func (r *peerResolver) fetchUser(userID int64) (*UserObj, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	r.c.Cache.Lock()
	if user, found := r.c.Cache.users[userID]; found {
		r.c.Cache.touchUserLRU(userID)
		r.c.Cache.Unlock()
		return user, nil
	}
	r.c.Cache.Unlock()

	userPeer, err := r.c.Cache.getUserPeer(userID)

	// if user is not in cache and if the bot is participant in the user, try with access hash = 0
	var inputPeerUser InputUser = &InputUserObj{UserID: userID, AccessHash: 0}
	if err == nil {
		inputPeerUser = userPeer
	}

	users, err := r.users([]InputUser{inputPeerUser})
	if err != nil {
		if r.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, contextErr(r.ctx, err)
		}
		// If fetch with cached access hash failed, retry with access hash = 0
		if inputPeerUser.(*InputUserObj).AccessHash != 0 {
			r.c.Cache.logger.WithFields(map[string]any{
				"user_id": userID,
				"error":   err.Error(),
			}).Debug("retrying user fetch with access_hash=0")
			users, err = r.users([]InputUser{&InputUserObj{UserID: userID, AccessHash: 0}})
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("no user with id '%d'", userID)
	}

	switch u := users[0].(type) {
	case *UserObj:
		r.c.Cache.UpdateUser(u)
		return u, nil
	case *UserEmpty:
		return nil, fmt.Errorf("user '%d' is unknown to this account; a valid access_hash is required to resolve them", userID)
	default:
		return nil, fmt.Errorf("expected UserObj for id '%d', but got %T", userID, users[0])
	}

}

func (r *peerResolver) fetchChannel(channelID int64) (*Channel, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	r.c.Cache.Lock()
	if channel, found := r.c.Cache.channels[channelID]; found {
		r.c.Cache.touchChannelLRU(channelID)
		r.c.Cache.Unlock()
		return channel, nil
	}
	r.c.Cache.Unlock()

	channelPeer, err := r.c.Cache.getChannelPeer(channelID)

	// if channel is not in cache and if the bot is participant in the channel, try with access hash = 0
	var inputChannel InputChannel = &InputChannelObj{ChannelID: channelID, AccessHash: 0}
	if err == nil {
		inputChannel = channelPeer
	}

	channels, err := r.channels([]InputChannel{inputChannel})
	if err != nil {
		if r.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, contextErr(r.ctx, err)
		}
		if inputChannel.(*InputChannelObj).AccessHash != 0 {
			r.c.Cache.logger.WithFields(map[string]any{
				"channel_id": channelID,
				"error":      err.Error(),
			}).Debug("retrying channel fetch with access_hash=0")
			channels, err = r.channels([]InputChannel{&InputChannelObj{ChannelID: channelID, AccessHash: 0}})
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	channelsObj, ok := channels.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("expected MessagesChatsObj for channel id '%d', but got different type", channelID)
	}

	if len(channelsObj.Chats) == 0 {
		return nil, fmt.Errorf("no channel with id '%d'", channelID)
	}

	switch ch := channelsObj.Chats[0].(type) {
	case *Channel:
		r.c.Cache.UpdateChannel(ch)
		return ch, nil
	case *ChannelForbidden:
		return nil, fmt.Errorf("channel '%d' is forbidden (access denied)", channelID)
	case *ChatEmpty:
		return nil, fmt.Errorf("channel '%d' is unknown to this account; a valid access_hash is required", channelID)
	default:
		return nil, fmt.Errorf("expected Channel for id '%d', but got %T", channelID, channelsObj.Chats[0])
	}

}

func (r *peerResolver) fetchChat(chatID int64) (*ChatObj, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	r.c.Cache.Lock()
	if chat, found := r.c.Cache.chats[chatID]; found {
		r.c.Cache.touchLRU(cachePeerKey{'g', chatID})
		r.c.Cache.Unlock()
		return chat, nil
	}
	r.c.Cache.Unlock()

	chat, err := r.chats([]int64{chatID})
	if err != nil {
		return nil, err
	}

	chatsObj, ok := chat.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("expected MessagesChatsObj for chat id %d, but got different type", chatID)
	}

	if len(chatsObj.Chats) == 0 {
		return nil, fmt.Errorf("no chat with id '%d'", chatID)
	}

	chatObj, ok := chatsObj.Chats[0].(*ChatObj)
	if !ok {
		return nil, fmt.Errorf("expected ChatObj for id '%d', but got different type", chatID)
	}
	r.c.Cache.UpdateChat(chatObj)

	return chatObj, nil

}

func (r *peerResolver) inputPeer(peerID int64) (InputPeer, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	if peerID == 0 {
		return nil, errors.New("invalid peer ID 0: use GetMe to retrieve self or pass 'me'")
	}
	if peerID < -1000000000000 {
		channelID := trimSuffixHundred(peerID)
		r.c.Cache.Lock()
		if channelHash, ok := r.c.Cache.InputPeers.InputChannels[channelID]; ok {
			r.c.Cache.touchChannelLRU(channelID)
			r.c.Cache.Unlock()
			return &InputPeerChannel{channelID, channelHash}, nil
		}
		r.c.Cache.Unlock()

		// try to fetch from Telegram
		if channel, err := r.channel(channelID); err == nil {
			return &InputPeerChannel{channelID, channel.AccessHash}, nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, contextErr(r.ctx, err)
		}

		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		return nil, fmt.Errorf("there is no channel with id '%d' or missing from cache", peerID)
	}

	// chat id (negative)
	if peerID < 0 {
		chatID := peerID * -1
		r.c.Cache.Lock()
		_, chatExists := r.c.Cache.chats[chatID]
		if chatExists {
			r.c.Cache.touchLRU(cachePeerKey{'g', chatID})
		}
		r.c.Cache.Unlock()

		if chatExists {
			return &InputPeerChat{chatID}, nil
		}

		// try to fetch from Telegram
		if _, err := r.chat(chatID); err == nil {
			return &InputPeerChat{chatID}, nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, contextErr(r.ctx, err)
		}

		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		return nil, fmt.Errorf("there is no chat with id '%d' or missing from cache", peerID)
	}

	// user id (positive)
	r.c.Cache.Lock()
	userHash, userExists := r.c.Cache.InputPeers.InputUsers[peerID]
	if userExists {
		r.c.Cache.touchUserLRU(peerID)
	}
	r.c.Cache.Unlock()

	if userExists {
		return &InputPeerUser{peerID, userHash}, nil
	}

	// check if it's a channel without -100 prefix before hitting the network
	r.c.Cache.Lock()
	channelHash, channelExists := r.c.Cache.InputPeers.InputChannels[peerID]
	if channelExists {
		r.c.Cache.touchChannelLRU(peerID)
	}
	r.c.Cache.Unlock()

	if channelExists {
		return &InputPeerChannel{peerID, channelHash}, nil
	}

	user, err := r.user(peerID)
	if err == nil {
		return &InputPeerUser{peerID, user.AccessHash}, nil
	}

	return nil, fmt.Errorf("resolving peer '%d': %w", peerID, err)

}

func (r *peerResolver) resolvePeer(PeerID any) (InputPeer, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	if v := reflect.ValueOf(PeerID); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, fmt.Errorf("peer is a nil %T", PeerID)
	}
	switch p := PeerID.(type) {
	case *PeerChat:
		if p.ChatID == 0 {
			return nil, fmt.Errorf("invalid chat peer: missing ID")
		}
	case *PeerChannel:
		if p.ChannelID == 0 {
			return nil, fmt.Errorf("invalid channel peer: missing ID")
		}
	}
	peer, err := r.sendable(PeerID)
	if err != nil {
		return nil, err
	}

	switch p := peer.(type) {
	case nil:
		return nil, fmt.Errorf("could not resolve peer %T", PeerID)
	case *InputPeerUser:
		if p == nil || p.UserID == 0 {
			return nil, fmt.Errorf("invalid user peer: missing ID")
		}
	case *InputPeerChat:
		if p == nil || p.ChatID == 0 {
			return nil, fmt.Errorf("invalid chat peer: missing ID")
		}
	case *InputPeerChannel:
		if p == nil || p.ChannelID == 0 {
			return nil, fmt.Errorf("invalid channel peer: missing ID")
		}
	default:
		if v := reflect.ValueOf(peer); v.Kind() == reflect.Pointer && v.IsNil() {
			return nil, fmt.Errorf("could not resolve peer %T", PeerID)
		}
	}
	return peer, nil

}

func (r *peerResolver) sendable(PeerID any) (InputPeer, error) {
PeerSwitch:
	switch Peer := PeerID.(type) {
	case nil:
		return nil, fmt.Errorf("peer is nil, cannot send nil peer")
	case *PeerUser:
		if Peer == nil || Peer.UserID == 0 {
			return nil, fmt.Errorf("invalid user peer: missing ID")
		}
		peerEntity, err := r.peerUser(Peer.UserID)
		if err != nil {
			return nil, err
		}
		return &InputPeerUser{UserID: peerEntity.UserID, AccessHash: peerEntity.AccessHash}, nil
	case *PeerChat:
		return &InputPeerChat{ChatID: Peer.ChatID}, nil
	case *PeerChannel:
		peerEntity, err := r.peerChannel(Peer.ChannelID)
		if err != nil {
			return nil, err
		}
		return &InputPeerChannel{ChannelID: peerEntity.ChannelID, AccessHash: peerEntity.AccessHash}, nil
	case *InputPeerChat:
		return Peer, nil
	case *InputPeerChannel:
		return Peer, nil
	case *InputPeerUser:
		if Peer == nil || Peer.UserID == 0 {
			return nil, fmt.Errorf("invalid user peer: missing ID")
		}
		return Peer, nil
	case *InputPeer:
		if Peer == nil {
			return nil, fmt.Errorf("peer is nil")
		}
		return r.resolvePeer(*Peer)
	case *InputUserSelf:
		return &InputPeerSelf{}, nil
	case *InputPeerSelf:
		return Peer, nil
	case *InputPeerEmpty:
		return Peer, nil
	case *InputPeerUserFromMessage:
		return Peer, nil
	case *InputPeerChannelFromMessage:
		return Peer, nil
	case *InputUserObj:
		if Peer == nil || Peer.UserID == 0 {
			return nil, fmt.Errorf("invalid user peer: missing ID")
		}
		return &InputPeerUser{UserID: Peer.UserID, AccessHash: Peer.AccessHash}, nil
	case *InputChannelObj:
		return &InputPeerChannel{ChannelID: Peer.ChannelID, AccessHash: Peer.AccessHash}, nil
	case *ChatObj:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *Channel:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *UserObj:
		if Peer == nil || Peer.ID == 0 {
			return nil, fmt.Errorf("invalid user peer: missing ID; use GetMe to check lookup errors")
		}
		return &InputPeerUser{UserID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case int64, int32, int:
		peerEntity, err := r.inputPeer(getAnyInt(PeerID))
		if peerEntity == nil || err != nil {
			return nil, err
		}
		return peerEntity, nil
	case string:
		if i, err := strconv.ParseInt(Peer, 10, 64); err == nil {
			PeerID = i
			goto PeerSwitch
		}
		if Peer == "me" || Peer == "self" {
			return &InputPeerSelf{}, nil
		}

		if peerID, accessHash, isChannel, found := r.c.Cache.LookupUsername(Peer); found {
			if accessHash == 0 {
				r.c.Log.WithFields(map[string]any{
					"username": Peer,
					"peer_id":  peerID,
				}).Debug("username cache incomplete, refreshing from API")
			} else {
				if isChannel {
					return &InputPeerChannel{ChannelID: peerID, AccessHash: accessHash}, nil
				}
				return &InputPeerUser{UserID: peerID, AccessHash: accessHash}, nil
			}
		}

		peerEntity, err := r.resolveUsername(Peer)
		if err != nil {
			return nil, err
		}
		switch peerEntity := peerEntity.(type) {
		case *ChatObj:
			return &InputPeerChat{ChatID: peerEntity.ID}, nil
		case *Channel:
			return &InputPeerChannel{ChannelID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *UserObj:
			return &InputPeerUser{UserID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *ChannelForbidden:
			return &InputPeerChannel{ChannelID: peerEntity.ID, AccessHash: peerEntity.AccessHash}, nil
		case *ChatForbidden:
			return &InputPeerChat{ChatID: peerEntity.ID}, nil
		default:
			return nil, fmt.Errorf("unknown peer type %T", peerEntity)
		}
	case *UserFull:
		if peerEntity, err := r.peerUser(Peer.ID); err == nil {
			return &InputPeerUser{UserID: peerEntity.UserID, AccessHash: peerEntity.AccessHash}, nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		return nil, fmt.Errorf("failed to get sendable peer for UserFull, cannot find user in cache")
	case *ChatFullObj:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *ChannelFull:
		if peerEntity, err := r.peerChannel(Peer.ID); err == nil {
			return &InputPeerChannel{ChannelID: peerEntity.ChannelID, AccessHash: peerEntity.AccessHash}, nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		return nil, fmt.Errorf("failed to get sendable peer for ChannelFull, cannot find channel in cache")
	case *ChannelForbidden:
		return &InputPeerChannel{ChannelID: Peer.ID, AccessHash: Peer.AccessHash}, nil
	case *ChatForbidden:
		return &InputPeerChat{ChatID: Peer.ID}, nil
	case *InputUserFromMessage:
		return &InputPeerUserFromMessage{Peer: Peer.Peer, MsgID: Peer.MsgID, UserID: Peer.UserID}, nil
	case *InputChannelFromMessage:
		return &InputPeerChannelFromMessage{Peer: Peer.Peer, MsgID: Peer.MsgID, ChannelID: Peer.ChannelID}, nil
	default:
		return nil, fmt.Errorf("failed to get sendable peer, unknown type %s", fmt.Sprintf("%T", PeerID))
	}

}

func (r *peerResolver) resolveUsername(username string, ref ...string) (any, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	var referer = getVariadic(ref, "")

	raw, err := r.call(&ContactsResolveUsernameParams{Username: username, Referer: referer})
	if err != nil {
		return nil, fmt.Errorf("resolving username: %w", err)
	}
	resp, ok := raw.(*ContactsResolvedPeer)
	if !ok || resp == nil {
		return nil, fmt.Errorf("unexpected username response: %T", raw)
	}
	r.c.Cache.UpdatePeersToCache(resp.Users, resp.Chats)
	if len(resp.Users) != 0 {
		switch User := resp.Users[0].(type) {
		case *UserObj:
			return User, nil
		case *UserEmpty:
			return User, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else if len(resp.Chats) != 0 {
		switch Peer := resp.Chats[0].(type) {
		case *ChatObj:
			return Peer, nil
		case *Channel:
			return Peer, nil
		case *ChannelForbidden:
			return Peer, nil
		case *ChatForbidden:
			return Peer, nil
		default:
			return nil, fmt.Errorf("got wrong response: %s", reflect.TypeOf(resp).String())
		}
	} else {
		return nil, fmt.Errorf("no user or chat has username %s", username)
	}

}

func (r *peerResolver) getSender(FromID Peer) *UserObj {
	if FromID == nil {
		return &UserObj{}
	}
	switch FromID := FromID.(type) {
	case *PeerUser:
		u, err := r.user(FromID.UserID)
		r.remember(err)
		if err == nil {
			return u
		}
	case *PeerChat:
		u, err := r.chat(FromID.ChatID)
		r.remember(err)
		if err == nil {
			return &UserObj{ID: FromID.ChatID, FirstName: u.Title, LastName: "", Username: "", Phone: "", AccessHash: 0, Photo: nil, Status: nil, Bot: false, Verified: false, Restricted: false}
		}
	case *PeerChannel:
		u, err := r.channel(FromID.ChannelID)
		r.remember(err)
		if err == nil {
			return &UserObj{ID: FromID.ChannelID, AccessHash: u.AccessHash, Username: u.Username, FirstName: u.Title, LastName: "", Phone: "", Bot: false, Verified: false, LangCode: ""}
		}
	}
	return &UserObj{}

}

func (r *peerResolver) getChat(PeerID Peer) *ChatObj {
	switch PeerID := PeerID.(type) {
	case *PeerChat:
		chat, err := r.chat(PeerID.ChatID)
		r.remember(err)
		if err == nil {
			return chat
		}
	}
	return nil

}

func (r *peerResolver) getChannel(PeerID Peer) *Channel {
	switch PeerID := PeerID.(type) {
	case *PeerChannel:
		channel, err := r.channel(PeerID.ChannelID)
		r.remember(err)
		if err == nil {
			return channel
		}
	}
	return nil

}

func (r *peerResolver) getInputPeer(PeerID Peer) InputPeer {
	if PeerID == nil {
		return nil
	}
	switch PeerID := PeerID.(type) {
	case *PeerUser:
		u, err := r.user(PeerID.UserID)
		r.remember(err)
		if err == nil {
			return &InputPeerUser{UserID: PeerID.UserID, AccessHash: u.AccessHash}
		}
	case *PeerChat:
		return &InputPeerChat{ChatID: PeerID.ChatID}
	case *PeerChannel:
		u, err := r.channel(PeerID.ChannelID)
		r.remember(err)
		if err == nil {
			return &InputPeerChannel{ChannelID: PeerID.ChannelID, AccessHash: u.AccessHash}
		}
	}
	return nil

}

func (r *peerResolver) peerType(Peer any) string {
	switch Peer := Peer.(type) {
	case *PeerUser, *InputPeerUser, *InputUserObj, *InputUserFromMessage:
		return EntityUser
	case *PeerChat, *InputPeerChat:
		return EntityChat
	case *PeerChannel, *InputPeerChannel, *InputChannelObj, *InputChannelFromMessage:
		return EntityChannel
	case int64, int32, int:
		peerEntity, err := r.inputPeer(getAnyInt(Peer))
		r.remember(err)
		if peerEntity == nil || err != nil {
			return EntityUnknown
		}
		return r.peerType(peerEntity)
	case string:
		peerEntity, err := r.resolvePeer(Peer)
		r.remember(err)
		if err != nil {
			return EntityUnknown
		}
		return r.peerType(peerEntity)
	default:
		return EntityUnknown
	}

}

func (r *peerResolver) self() *UserObj {
	if me := r.c.getMe(); me != nil {
		return me
	}
	users, err := r.users([]InputUser{&InputUserSelf{}})
	r.remember(err)
	if err != nil || len(users) != 1 {
		return nil
	}
	me, ok := users[0].(*UserObj)
	if !ok || me == nil || me.ID == 0 {
		return nil
	}
	r.c.setMe(me)
	if r.c.Cache != nil {
		if err := r.c.Cache.BindToUser(me.ID); err != nil && r.c.Log != nil {
			r.c.Log.WithError(err).Warn("failed to bind cache to user")
		}
	}
	return me
}

// PackMessagesContext wraps messages using ctx for all cold metadata lookups.
func PackMessagesContext(ctx context.Context, c *Client, messages []Message) ([]*NewMessage, error) {
	return c.peerResolver(ctx, nil).packMessages(messages)
}
func (r *peerResolver) packMessages(messages []Message) ([]*NewMessage, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]*NewMessage, 0, len(messages))
	for _, raw := range messages {
		m := r.packMessage(raw)
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		if r.err != nil {
			return nil, r.err
		}
		if m != nil {
			result = append(result, m)
		}
	}
	return result, nil
}

func (r *peerResolver) packMessage(message Message) *NewMessage {
	var (
		m = &NewMessage{}
	)
	switch message := message.(type) {
	case *MessageObj:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Message = message
		m.Client = r.c
	case *MessageService:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Client = r.c
		m.Message = &MessageObj{
			Out: message.Out, Mentioned: message.Mentioned, MediaUnread: message.MediaUnread, Silent: message.Silent, ID: message.ID, FromID: message.FromID, PeerID: message.PeerID, Date: message.Date, Message: "", Post: message.Post, FromScheduled: false, ReplyTo: message.ReplyTo, FwdFrom: nil, ViaBotID: 0, Legacy: false, EditHide: false, GroupedID: 0, ReplyMarkup: nil,
		}
		m.Action = message.Action
	case *MessageEmpty:
		m.ID = message.ID
		m.OriginalUpdate = message
		m.Client = r.c
		m.Message = &MessageObj{ID: message.ID, PeerID: message.PeerID, FromID: &PeerUser{}}
		m.Action = &MessageActionEmpty{}
	default:
		return nil
	}

	// Set the chat and channel information
	m.Chat = r.getChat(m.Message.PeerID)
	m.Channel = r.getChannel(m.Message.PeerID)

	if m.Channel != nil && m.Channel.Min {
		if cached, err := r.channel(m.Channel.ID); err == nil && cached != nil && !cached.Min {
			m.Channel = cached
		} else if actualChannel, err := r.channels([]InputChannel{&InputChannelObj{ChannelID: m.Channel.ID, AccessHash: m.Channel.AccessHash}}); err == nil {
			switch actualChannels := actualChannel.(type) {
			case *MessagesChatsObj:
				if len(actualChannels.Chats) > 0 {
					if channelActual, ok := actualChannels.Chats[0].(*Channel); ok {
						r.c.Cache.UpdateChannel(channelActual)
						m.Channel = channelActual
					}
				}
			case *MessagesChatsSlice:
				if len(actualChannels.Chats) > 0 {
					if channelActual, ok := actualChannels.Chats[0].(*Channel); ok {
						r.c.Cache.UpdateChannel(channelActual)
						m.Channel = channelActual
					}
				}
			}
		}
	} else if m.Channel != nil && !m.Channel.Min {
		r.c.Cache.UpdateChannel(m.Channel)
	}

	if m.Message.Out {
		if me := r.self(); me != nil {
			m.Sender = me
		} else if m.Message.FromID != nil {
			m.Sender = r.getSender(m.Message.FromID)
		}
	} else if _, empty := message.(*MessageEmpty); !empty {
		if m.Message.FromID != nil {
			m.Sender = r.getSender(m.Message.FromID)
			if m.Sender != nil && m.Sender.Min {
				if actualUser, err := r.users([]InputUser{&InputUserFromMessage{
					Peer:   r.getInputPeer(m.Message.PeerID),
					MsgID:  m.ID,
					UserID: m.Sender.ID,
				}}); err == nil && len(actualUser) > 0 {
					if userActual, ok := actualUser[0].(*UserObj); ok {
						r.c.Cache.UpdateUser(userActual)
						m.Sender = userActual
					}
				}
			} else if m.Sender != nil && !m.Sender.Min {
				r.c.Cache.UpdateUser(m.Sender)
			}
		} else {
			m.Sender = r.getSender(m.Message.PeerID)
		}
	}

	if (m.Channel != nil && m.Sender != nil) && (m.Sender.ID == m.Channel.ID) || (m.Message.FromID != nil && r.peerType(m.Message.FromID) == EntityChannel) {
		m.SenderChat = r.getChannel(m.Message.FromID)
	} else {
		m.SenderChat = &Channel{}
	}

	if m.Chat == nil && m.Sender != nil {
		m.Chat = &ChatObj{ID: m.Sender.ID, Title: m.Sender.FirstName} // private user/bot chat
	}

	m.Peer = r.getInputPeer(m.Message.PeerID)
	if m.IsMedia() {
		FileID := PackBotFileID(m.Media())
		m.File = &CustomFile{
			FileID: FileID,
			Name:   GetFileName(m.Media()),
			Size:   GetFileSize(m.Media()),
			Ext:    GetFileExt(m.Media()),
		}
	}
	return m

}
