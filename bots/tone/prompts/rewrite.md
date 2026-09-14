You are editing one piece of text so that it reads like a person wrote it. You are not rewriting it, not improving the argument, and not adding anything. Every fact, name, number, link and claim in the input must survive unchanged.

The text:

```
{{text}}
```

Produce **only** JSON in this shape:

```json
{
  "text": "the edited text",
  "changed": ["one short line per kind of thing you fixed"]
}
```

## What to remove

**Em dashes.** Replace with a comma, a full stop, a colon, or brackets, whichever the sentence actually wants. Never leave `—`, `--`, or an en dash `–` doing an em dash's job.

**Stock vocabulary.** delve, robust, comprehensive, nuanced, multifaceted, crucial, pivotal, leverage (as a verb), landscape, realm, tapestry, testament, underscore, seamless, elevate, unlock, harness (as a verb), foster, myriad, plethora, resonate, meticulous. Say the plain word instead.

**Stock constructions.**
- "It's not just X, it's Y" and "not only X but also Y"
- "In today's fast-paced world", "In an era of", "More than ever"
- "Let's dive in", "Let's explore", "Here's the thing:", "The truth is"
- "It's worth noting that", "It's important to remember that". Just say the thing.
- "I hope this helps", "Feel free to", "Don't hesitate to"
- Opening on a rhetorical question when the answer is the next sentence

**Three-part lists used as decoration**, like "clear, concise, and compelling". Two items, or one, unless the third carries real weight.

**Over-signposting.** Firstly / Secondly / In conclusion / To summarise. Cut the closing paragraph that restates what was just said.

**Uniform rhythm.** Models write sentences of near-identical length. Vary them. A short one lands.

## What not to do

- Don't compensate with slang, contractions you wouldn't use, or forced casualness. Removing the tells makes it sound human; adding "honestly" and "kinda" makes it sound like a different model.
- Don't change the register. A release note stays a release note.
- Don't cut substance to prove you edited. Removing filler will shorten the text, sometimes a lot, and that is the job working. Losing a fact, a name, a number or a caveat is not.
- Don't touch anything inside code blocks, inline code, URLs, or quoted material.
- If a phrase on the list above is genuinely the right word (a real tapestry, an actual landscape), keep it.

## changed

One short line per kind of fix, in plain language: `"removed 3 em dashes"`, `"cut the closing summary paragraph"`, `"replaced 'leverage' with 'use'"`. If the text was already fine, return it byte for byte and set `changed` to `[]`. Saying you changed nothing is a real answer and a better one than inventing an edit.

Output raw JSON only.

{{instructions}}
