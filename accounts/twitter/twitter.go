// Package twitter is a Twitter account for Telephant.
package twitter

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ChimeraCoder/anaconda"

	"github.com/nueh/telephant/accounts"
)

const (
	initialFeedCount          = 200
	initialNotificationsCount = 50
)

// Account is a twitter account for Telephant.
type Account struct {
	consumerKey       string
	consumerSecret    string
	accessToken       string
	accessTokenSecret string

	twitterAPI *anaconda.TwitterApi
	self       anaconda.User

	evchan  chan interface{}
	SigChan chan bool
}

// NewAccount returns a new twitter account.
func NewAccount(consumerKey, consumerSecret, accessToken, accessTokenSecret string) *Account {
	return &Account{
		consumerKey:       consumerKey,
		consumerSecret:    consumerSecret,
		accessToken:       accessToken,
		accessTokenSecret: accessTokenSecret,
	}
}

// handleAnacondaError is a helper function to rate-limit twitter requests.
func (mod *Account) handleAnacondaError(err error, msg string) {
	if err != nil {
		switch e := err.(type) {
		case *anaconda.ApiError:
			isRateLimitError, nextWindow := e.RateLimitCheck()
			if isRateLimitError {
				log.Println("Oops, I exceeded the API rate limit!")
				waitPeriod := nextWindow.Sub(time.Now())
				log.Printf("waiting %f seconds to next window!", waitPeriod.Seconds())
				time.Sleep(waitPeriod)
			} else {
				if msg != "" {
					log.Printf("Error: %s (%+v)", msg, err)
					panic(msg)
				}
			}
		default:
			log.Printf("Error: %s (%+v)", msg, err)
			panic(msg)
		}
	}
}

// Run executes the account's event loop.
func (mod *Account) Run(eventChan chan interface{}) {
	mod.evchan = eventChan

	anaconda.SetConsumerKey(mod.consumerKey)
	anaconda.SetConsumerSecret(mod.consumerSecret)
	mod.twitterAPI = anaconda.NewTwitterApi(mod.accessToken, mod.accessTokenSecret)
	mod.twitterAPI.ReturnRateLimitError(true)
	defer mod.twitterAPI.Close()

	// Test the credentials on startup
	credentialsVerified := false
	for !credentialsVerified {
		ok, err := mod.twitterAPI.VerifyCredentials()
		mod.handleAnacondaError(err, "Could not verify Twitter API Credentials")
		credentialsVerified = ok
	}

	var err error
	mod.self, err = mod.twitterAPI.GetSelf(url.Values{})
	mod.handleAnacondaError(err, "Could not get own user object from Twitter API")

	ev := accounts.LoginEvent{
		Username: mod.self.ScreenName,
		Name:     mod.self.Name,
		Avatar:   mod.self.ProfileImageUrlHttps,
	}
	mod.evchan <- ev

	v := url.Values{}
	v.Set("count", strconv.FormatInt(initialFeedCount, 10))
	tweets, err := mod.twitterAPI.GetHomeTimeline(v)
	mod.handleAnacondaError(err, "Could not get timeline from Twitter API")
	for i := len(tweets) - 1; i >= 0; i-- {
		mod.handleStreamEvent(tweets[i])
	}

	v.Set("count", strconv.FormatInt(initialNotificationsCount, 10))
	tweets, err = mod.twitterAPI.GetMentionsTimeline(v)
	mod.handleAnacondaError(err, "Could not get mention feed from Twitter API")
	for i := len(tweets) - 1; i >= 0; i-- {
		mod.handleStreamEvent(tweets[i])
	}

	mod.handleStream()
}

// Post posts a new status
func (mod *Account) Post(message string) error {
	_, err := mod.twitterAPI.PostTweet(message, url.Values{})
	return err
}

// Reply posts a new reply-tweet
func (mod *Account) Reply(replyid int64, message string) error {
	v := url.Values{}
	v.Set("in_reply_to_status_id", strconv.FormatInt(replyid, 10))
	_, err := mod.twitterAPI.PostTweet(message, v)
	return err
}

// Retweet posts a retweet
func (mod *Account) Retweet(id int64) error {
	_, err := mod.twitterAPI.Retweet(id, true)
	return err
}

// Like likes a tweet
func (mod *Account) Like(id int64) error {
	_, err := mod.twitterAPI.Favorite(id)
	return err
}

// handleRetweetStatus strips the RT prefix from the post.
func handleRetweetStatus(status string) string {
	if strings.HasPrefix(status, "RT ") && strings.Count(status, " ") >= 2 {
		return strings.Join(strings.Split(status, " ")[2:], " ")
	}

	return status
}

// handleReplyStatus strips the reply prefix from the post.
func handleReplyStatus(status string) string {
	if strings.HasPrefix(status, "@") && strings.Index(status, " ") > 0 {
		return status[strings.Index(status, " "):]
	}

	return status
}

// parseTweet prepares the body of a post before it can be displayed.
func parseTweet(ents anaconda.Entities, ev *accounts.MessageEvent) {
	for _, u := range ents.Urls {
		r := fmt.Sprintf("<a style=\"text-decoration: none; color: orange;\" href=\"%s\">%s</a>", u.Expanded_url, u.Display_url)
		ev.Post.Body = strings.Replace(ev.Post.Body, u.Url, r, -1)
	}
	for _, media := range ents.Media {
		ev.Media = append(ev.Media, accounts.Media{
			Preview: media.Media_url_https,
			URL:     media.Media_url_https,
		})
		ev.Post.Body = strings.Replace(ev.Post.Body, media.Url, "", -1)
		// FIXME:
		break
	}
}

// handleStreamEvent handles incoming API events.
func (mod *Account) handleStreamEvent(item interface{}) {
	switch status := item.(type) {
	case anaconda.Tweet:
		// log.Printf("Tweet: %s %s", status.FullText, status.User.ScreenName)

		ev := accounts.MessageEvent{
			Account: "twitter",
			Name:    "tweet",
			Post: accounts.Post{
				MessageID:  status.IdStr,
				Body:       status.FullText,
				Author:     status.User.ScreenName,
				AuthorName: status.User.Name,
				Avatar:     status.User.ProfileImageUrlHttps,
				CreatedAt:  time.Now(),
				URL:        "https://twitter.com/statuses/" + status.IdStr,
			},
		}

		if t, err := status.CreatedAtTime(); err == nil {
			ev.Post.CreatedAt = t
		}

		if status.InReplyToStatusID > 0 {
			ev.Reply = true
			ev.Post.Body = handleReplyStatus(ev.Post.Body)
			ev.Post.ReplyToID = status.InReplyToStatusIdStr
			ev.Post.ReplyToAuthor = status.InReplyToScreenName
		}

		for _, mention := range status.Entities.User_mentions {
			if mention.Screen_name == mod.self.ScreenName {
				ev.Notification = true
				if status.RetweetedStatus == nil {
					// someone mentioned us
					ev.Mention = true
				}
				break
			}
		}

		if status.RetweetedStatus != nil {
			// a retweet
			ev.Forward = true
			ev.Post.Body = handleRetweetStatus(status.RetweetedStatus.FullText)
			ev.Post.Author = status.RetweetedStatus.User.ScreenName
			ev.Post.AuthorName = status.RetweetedStatus.User.Name
			ev.Post.Avatar = status.RetweetedStatus.User.ProfileImageUrlHttps
			ev.Post.Actor = status.User.ScreenName
			ev.Post.ActorName = status.User.Name
			parseTweet(status.RetweetedStatus.Entities, &ev)
			parseTweet(status.RetweetedStatus.ExtendedEntities, &ev)
		}
		parseTweet(status.Entities, &ev)
		parseTweet(status.ExtendedEntities, &ev)

		mod.evchan <- ev

	case anaconda.EventTweet:
		log.Printf("Event: %s %s", status.TargetObject.FullText, status.Source.ScreenName)

		ev := accounts.MessageEvent{
			Account: "twitter",
			Name:    "tweet",
			Post: accounts.Post{
				MessageID:  status.TargetObject.IdStr,
				Body:       status.TargetObject.FullText,
				Author:     status.Source.ScreenName,
				AuthorName: status.Source.Name,
				Avatar:     status.Source.ProfileImageUrlHttps,
				CreatedAt:  time.Now(),
				URL:        "https://twitter.com/statuses/" + status.TargetObject.IdStr,
			},
		}

		if t, err := status.TargetObject.CreatedAtTime(); err == nil {
			ev.Post.CreatedAt = t
		}

		switch status.Event.Event {
		case "retweeted_retweet":
			ev.Forward = true
			ev.Post.Body = handleRetweetStatus(ev.Post.Body)
			ev.Post.Author = status.TargetObject.User.ScreenName
			ev.Post.AuthorName = status.TargetObject.User.Name
			ev.Post.Avatar = status.TargetObject.User.ProfileImageUrlHttps
			ev.Post.Actor = status.Source.ScreenName
			ev.Post.ActorName = status.Source.Name
			if status.TargetObject.RetweetedStatus.User.ScreenName == mod.self.ScreenName {
				ev.Notification = true
			}

			parseTweet(status.TargetObject.RetweetedStatus.Entities, &ev)

		case "favorited_retweet":
			ev.Forward = true
			ev.Like = true
			ev.Post.Body = handleRetweetStatus(ev.Post.Body)
			ev.Post.Author = status.TargetObject.User.ScreenName
			ev.Post.AuthorName = status.TargetObject.User.Name
			ev.Post.Avatar = status.TargetObject.User.ProfileImageUrlHttps
			ev.Post.Actor = status.Source.ScreenName
			ev.Post.ActorName = status.Source.Name
			if status.TargetObject.RetweetedStatus.User.ScreenName == mod.self.ScreenName {
				ev.Notification = true
			}

			parseTweet(status.TargetObject.RetweetedStatus.Entities, &ev)

		case "favorite":
			ev.Like = true

			ev.Post.Author = status.TargetObject.User.ScreenName
			ev.Post.AuthorName = status.TargetObject.User.Name
			ev.Post.Avatar = status.TargetObject.User.ProfileImageUrlHttps
			ev.Post.Actor = status.Source.ScreenName
			ev.Post.ActorName = status.Source.Name

			if status.TargetObject.User.ScreenName == mod.self.ScreenName {
				ev.Notification = true
			}
		/* case "unfavorited_retweeted":
			fallthrough
		case "unfavorite":
			fallthrough */
		default:
			log.Println("Unhandled event type", status.Event.Event)
			log.Printf("Event Tweet: %+v", status)
			return
		}

		for _, mention := range status.TargetObject.Entities.User_mentions {
			if mention.Screen_name == mod.self.ScreenName {
				ev.Notification = true
				break
			}
		}

		mod.evchan <- ev

	case anaconda.LimitNotice:
		log.Printf("Limit: %+v", status)
	case anaconda.DisconnectMessage:
		log.Printf("Disconnect: %+v", status)
	case anaconda.UserWithheldNotice:
		log.Printf("User Withheld: %+v", status)
	case anaconda.StatusWithheldNotice:
		log.Printf("Status Withheld: %+v", status)
	case anaconda.Friendship:
		log.Printf("Friendship: %s", status.Screen_name)
	case anaconda.Relationship:
		log.Printf("Relationship: %s", status.Source.Screen_name)
	case anaconda.Event:
		log.Printf("Event: %+v", status)
	default:
		// log.Printf("Unhandled type %+v", item)
	}
}

// handleStream handles all connected Twitter API streams.
func (mod *Account) handleStream() {
	s := mod.twitterAPI.UserStream(url.Values{})

	for {
		select {
		case <-mod.SigChan:
			return
		case item := <-s.C:
			mod.handleStreamEvent(item)
		}
	}
}
