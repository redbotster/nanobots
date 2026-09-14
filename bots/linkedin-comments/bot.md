# linkedin-comments

Fetch the comments on one of your LinkedIn posts.

`bare` harness, one `service.call`. Reads only: `writes_allowed` is empty.

Pairs with `comment-responder`, which takes `list<json>` comments from any source:

```yaml
- from: reader.comments
  to: responder.comments
```

`post_urn` is the full URN, e.g. `urn:li:share:7123456789`. `post-publisher` hands one back when it publishes, so a swarm can publish and then come back to read the replies.

## This needs an approval LinkedIn grants per app

Reading comments uses the Community Management API. That is a separate product you add to your LinkedIn app, and LinkedIn reviews and approves it per app — it is not part of the default "Sign In with LinkedIn" scopes an OAuth connect gives you.

Until that approval lands, the API answers 403 and this bot stays on demo fixtures. The client says so in those words rather than surfacing a bare status code, because "403" and "you need to apply for a product" are very different problems.

`author` is the commenter's URN, not a name. Turning it into a display name is a second call per commenter, which this bot does not make: it would multiply the request count for something `comment-responder` does not need.
