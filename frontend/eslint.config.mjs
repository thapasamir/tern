import boundaries from "eslint-plugin-boundaries";
import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

/*
 * The architecture, enforced. Dependencies point one way:
 *
 *   app  ->  features  ->  shared
 *
 * - `app` holds routes only: thin pages that compose features.
 * - A feature may use `shared` and itself, never another feature. If two features need the same
 *   thing, it belongs in `shared`.
 * - `shared` knows nothing about features or routes.
 */
const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries },
    settings: {
      "import/resolver": { typescript: { project: "./tsconfig.json" } },
      "boundaries/elements": [
        { type: "shared", pattern: "src/shared" },
        { type: "feature", pattern: "src/features/*", capture: ["name"] },
        { type: "app", pattern: "src/app" },
      ],
    },
    rules: {
      "boundaries/dependencies": [
        "error",
        {
          default: "disallow",
          policies: [
            { from: { element: { type: "shared" } }, allow: { to: { element: { type: "shared" } } } },
            {
              from: { element: { type: "feature" } },
              allow: {
                to: [
                  { element: { type: "shared" } },
                  { element: { type: "feature", captured: { name: "{{ from.element.captured.name }}" } } },
                ],
              },
              message: "A feature may only use `shared` and itself. Move what two features share into `shared`.",
            },
            {
              from: { element: { type: "app" } },
              allow: {
                to: [{ element: { type: "app" } }, { element: { type: "feature" } }, { element: { type: "shared" } }],
              },
            },
          ],
        },
      ],
    },
  },
  globalIgnores([".next/**", "out/**", "build/**", "next-env.d.ts", "src/shared/api/schema.d.ts"]),
]);

export default eslintConfig;
