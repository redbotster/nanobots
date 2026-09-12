# newsletter-drafter

Assemble a newsletter draft from links, notes, and recent posts.

`openclaw` harness: one `ai.generate` call turns `sources`/`blurbs` into a subject, intro, and sections; those get rendered to an HTML file (`draft_html`) and also saved as a Gmail draft (`draft_id`) with the intro as the body — drafts only, never sent.

`template` (an optional file input) is accepted but not read yet — the same disclosed gap other bots' unused optional file inputs have.

`inputs.template` was declared and never read, for the same reason `render-pdf`'s was: the render step hardcodes `./templates/newsletter.html`, and a file input can't override a template path. Removed rather than left as a port that discards its input.
