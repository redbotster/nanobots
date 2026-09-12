# render-pdf

Render Markdown or HTML to a PDF file.

`bare` harness — no model — but rendering a real PDF needs real Chrome, which the distroless `bare` image doesn't have. The runner knows this (`internal/runner.needsBrowser`, via `imageFor`) and substitutes the `openclaw` image at execution time; the declared harness stays `bare` because that's still an honest description of what this bot decides for itself — nothing, it just renders.

`inputs.content` is wrapped as preformatted text (line breaks preserved, no markdown parsing yet — that's a real gap, not a design choice, tracked as a TODO rather than silently mis-rendering). `inputs.template` (an optional custom HTML template) isn't wired up in this build; every call uses the bundled `templates/plain.html`.

`inputs.template` was declared and never read — `transform.render` takes a template *path inside the bot*, and this bot hardcodes `./templates/plain.html`. Accepting a template file would need render to take content rather than a path, which isn't built. The port is gone rather than left silently swallowing whatever you snap into it.
