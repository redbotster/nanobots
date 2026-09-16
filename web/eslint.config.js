import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";
import prettier from "eslint-config-prettier";

// Lint rules, kept to the ones that catch bugs rather than opinions.
//
// The Go half of this repo has had `go vet` and a gofmt check for a while;
// the web half had neither, so a whole class of mistake was only ever caught
// by someone reading the diff. The rules-of-hooks check alone is worth the
// dependency: a stale closure or a missing dependency is exactly the kind of
// defect that renders fine, passes tests, and is wrong at 3am.
//
// Formatting questions are Prettier's, not ESLint's — `prettier` last in the
// list turns off every rule the two would argue about, so there is one tool
// with an opinion about layout and one with an opinion about correctness.
export default tseslint.config(
  { ignores: ["dist", "coverage", "node_modules"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser, ...globals.es2021 },
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,

      // react-hooks v7 folds the React Compiler's rule set into
      // `recommended`. Most of it is kept — rules-of-hooks,
      // set-state-in-render, purity, error-boundaries and the rest are all
      // on and all clean. Three are off, and only after reading every one
      // of the eighteen things they flagged here. None was a bug:
      //
      //   refs / immutability — `cancelConnectionRef.current = cleanup`
      //     inside a mousedown handler. Assigning a ref outside render is
      //     the documented way to do this; the rules exist for a compiler
      //     that may reorder around it, which this build does not use.
      //
      //   set-state-in-effect — resetting derived state when a prop
      //     changes, e.g. SwarmView clearing its plan and adopting the new
      //     swarm's last run. React's advice is a `key` or derived state
      //     instead, which is a real refactor of working code rather than
      //     a defect to fix.
      //
      // Turn them back on when this app adopts the compiler; until then
      // they report eighteen findings and no bugs, and a lint run nobody
      // can get to zero is a lint run nobody reads.
      "react-hooks/refs": "off",
      "react-hooks/immutability": "off",
      "react-hooks/set-state-in-effect": "off",
      // Vite's fast refresh only works when a module exports components and
      // nothing else. A warning rather than an error: several files here
      // deliberately export a helper beside their component so it can be
      // tested directly (swarmMatches, LastRunLine), and that is a trade
      // worth making knowingly.
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      // An unused name is nearly always a leftover, but a deliberately
      // ignored one is a real pattern — `catch {}` and `_unused` say so.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrors: "none" },
      ],
    },
  },
  {
    // Tests reach for `any` when building fixtures for types whose full
    // shape is the server's business, and casting through unknown on every
    // one of them costs more than it catches.
    files: ["**/*.test.{ts,tsx}"],
    rules: { "@typescript-eslint/no-explicit-any": "off" },
  },
  prettier,
);
