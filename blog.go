package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DiscordGophers/dr-docso/blog"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

func (b *botState) updateArticles() {
	articles, err := blog.Articles(http.DefaultClient)
	if err != nil {
		panic(err)
	}
	b.articles = articles

	articleTicker := time.NewTicker(time.Hour * 72)
	for {
		<-articleTicker.C
		articles, err := blog.Articles(http.DefaultClient)
		if err != nil {
			log.Printf("Error querying maps: %v", err)
			continue
		}

		b.articles = articles
	}
}

func (b *botState) handleBlog(e *gateway.InteractionCreateEvent, d *discord.CommandInteraction) {
	// only arg and required, always present
	query := d.Options[0].String()

	log.Printf("%s used blog(%q)", e.User.Tag(), query)

	if len(query) < 3 || len(query) > 20 {
		embed := failEmbed("Error", "Your query must be between 3 and 20 characters.")
		b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Flags:  api.EphemeralResponse,
				Embeds: &[]discord.Embed{embed},
			},
		})
		return
	}

	fromTitle, fromDesc, total := blog.MatchAll(b.articles, query)
	articles := append(fromTitle, fromDesc...)
	fields, opts := articleFields(articles)

	switch total {
	case 0:
		b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Flags:  api.EphemeralResponse,
				Embeds: &[]discord.Embed{failEmbed("Error", fmt.Sprintf("No results found for %q", query))},
			},
		})
		return

	case 1:
		b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Embeds: &[]discord.Embed{articles[0].Display()},
			},
		})
		return

	case 2:
		b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.MessageInteractionWithSource,
			Data: &api.InteractionResponseData{
				Embeds: &[]discord.Embed{
					{
						Title:  fmt.Sprintf("Blog: %q", query),
						Fields: fields,
						Color:  accentColor,
					},
				},
			},
		})
		return
	}

	comps := make(discord.ContainerComponents, 1, 2)
	if total > 5 {
		opts = opts[:5]
		fields = fields[:5]
		comps = append(comps, paginateButtons(query))
	}

	comps[0] = &discord.ActionRowComponent{
		&discord.SelectComponent{
			CustomID:    "blog.display",
			Options:     opts,
			Placeholder: "Display Blog Post",
		},
	}

	p := int(math.Ceil(float64(total) / float64(5)))
	b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Flags: api.EphemeralResponse,
			Embeds: &[]discord.Embed{
				{
					Title: fmt.Sprintf("Blog: %d Results", total),
					Footer: &discord.EmbedFooter{
						Text: fmt.Sprintf("Page 1 of %d\nTo display publicly, select a single post", p),
					},
					Fields: append([]discord.EmbedField{
						{
							Name:  "Search Term",
							Value: fmt.Sprintf("%q", query),
						},
					}, fields...),
					Color: accentColor,
				},
			},
			Components: &comps,
		},
	})
}

func (b *botState) handleBlogComponent(e *gateway.InteractionCreateEvent, data discord.ComponentInteraction, cmd string) {
	switch cmd {
	case "display":
		b.BlogDisplay(e, data.(*discord.SelectInteraction).Values[0])
		return
	}

	split := strings.SplitN(cmd, ".", 2)
	query := split[1]

	embed := e.Message.Embeds[0]
	matches := pageRe.FindStringSubmatch(embed.Footer.Text)
	if len(matches) != 3 {
		return
	}

	cur, _ := strconv.Atoi(matches[1])

	switch split[0] {
	case "prev":
		cur--
	case "next":
		cur++
	}

	fromTitle, fromDesc, total := blog.MatchAll(b.articles, query)
	p := int(math.Ceil(float64(total) / float64(5)))
	if cur < 1 || cur > p {
		b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
			Type: api.UpdateMessage, Data: &api.InteractionResponseData{},
		})
		return
	}
	fields, opts := articleFields(append(fromTitle, fromDesc...))
	if cur != p {
		fields = fields[(cur-1)*5 : cur*5]
		opts = opts[(cur-1)*5 : cur*5]
	} else {
		fields = fields[(cur-1)*5:]
		opts = opts[(cur-1)*5:]
	}

	comps := discord.Components(
		&discord.SelectComponent{
			CustomID:    "blog.display",
			Options:     opts,
			Placeholder: "Display Blog Post",
		},
		paginateButtons(query),
	)

	b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.UpdateMessage,
		Data: &api.InteractionResponseData{
			Flags: api.EphemeralResponse,
			Embeds: &[]discord.Embed{
				{
					Title: fmt.Sprintf("Blog: %d Results", total),
					Footer: &discord.EmbedFooter{
						Text: fmt.Sprintf("Page %d of %d\nTo display publicly, select a single post", cur, p),
					},
					Fields: append([]discord.EmbedField{
						{
							Name:  "Search Term",
							Value: fmt.Sprintf("%q", query),
						},
					}, fields...),
					Color: accentColor,
				},
			},
			Components: &comps,
		},
	})
}

var pageRe = regexp.MustCompile(`Page (\d+) of (\d+)`)

func (b *botState) BlogDisplay(e *gateway.InteractionCreateEvent, url string) {
	var article blog.Article
	for _, a := range b.articles {
		if a.URL == url {
			article = a
			break
		}
	}

	if article.URL == "" {
		return
	}

	b.state.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
		Type: api.UpdateMessage,
		Data: &api.InteractionResponseData{
			Components: &discord.ContainerComponents{},
		},
	})
	b.state.CreateInteractionFollowup(e.AppID, e.Token, api.InteractionResponseData{
		Content:         option.NewNullableString(e.User.Mention() + ":"),
		Embeds:          &[]discord.Embed{article.Display()},
		AllowedMentions: &api.AllowedMentions{},
	})
}

func articleFields(articles []blog.Article) (fields []discord.EmbedField, opts []discord.SelectOption) {
	for _, a := range articles {
		fields = append(fields, discord.EmbedField{
			Name:  fmt.Sprintf("%s, %s", a.Title, a.Date),
			Value: fmt.Sprintf("*%s*\n%s\n%s", a.Authors, a.Summary, a.URL),
		})

		opts = append(opts, discord.SelectOption{
			Label:       a.Title,
			Value:       a.URL,
			Description: a.Authors,
		})
	}
	return
}

func paginateButtons(query string) *discord.ActionRowComponent {
	return &discord.ActionRowComponent{
		&discord.ButtonComponent{
			Label:    "Prev Page",
			CustomID: discord.ComponentID("blog.prev." + query),
			Style:    discord.SecondaryButtonStyle(),
			Emoji:    &discord.ComponentEmoji{Name: "⬅️"},
		},
		&discord.ButtonComponent{
			Label:    "Next Page",
			CustomID: discord.ComponentID("blog.next." + query),
			Style:    discord.SecondaryButtonStyle(),
			Emoji:    &discord.ComponentEmoji{Name: "➡️"},
		},
	}
}
