# Tern frontend

The dashboard for Tern, the webhook delivery platform in `../backend`. Next.js (App Router) and TypeScript in strict mode, with Tailwind and shadcn/ui, restyled to the Tern design.

```bash
cp .env.example .env.local   # API_URL, where the backend listens
pnpm install
pnpm dev                     # http://localhost:3000, with the backend running
pnpm check                   # lint, typecheck, tests, formatting: run before committing
pnpm build
```

`/styleguide` shows every shared component. It exists in development only.

## Structure

```
src/
  app/               routes only: thin pages that compose features
    (dashboard)/     the signed-in app, under the masthead
  features/<name>/   one folder per domain, like the backend
    components/      what the feature draws
    api.ts           its calls to the backend, the only place that makes them
    actions.ts       server actions: its writes
    schemas.ts       Zod schemas for its forms, also used to check them on the server
    index.ts         what the feature offers the rest of the app
  shared/
    api/             typed client, ApiError, types generated from the OpenAPI document
    config/          the server's environment, checked at startup
    lib/             small helpers
    ui/              shadcn components and our own, in the Tern style
```

### The rules

1. **Dependencies point one way: `app -> features -> shared`.** A feature may use `shared` and itself, never another feature. If two features need the same thing, it moves to `shared`. ESLint enforces this (`eslint.config.mjs`), so it cannot erode quietly.
2. **Pages hold no logic.** A page fetches through a feature and lays out its components, as a controller calls a service.
3. **Only a feature's `api.ts` talks to the backend,** and only on the server. The browser never sees the API URL or the session token.
4. **Types come from the API.** `src/shared/api/schema.d.ts` is generated from `backend/internal/api/openapi.yaml` with `pnpm gen:api`; change the API and the compiler shows what to update. CI fails if the file is stale.

## Data flow: server first

- Pages are Server Components and read data on the server with the session cookie. Writes are Server Actions.
- Filters and pagination live in the URL, so a filtered view can be shared and survives a refresh.
- Client components are small islands for what has to be interactive or live. TanStack Query is added to a screen only if it proves it needs it.
- This Next.js has Cache Components on: anything that reads request data (cookies, uncached fetches) must sit inside a `<Suspense>` boundary, or the build says so. Keep layouts static and put the dynamic part in its own boundary.

## Design

"A logbook for things that travel." The tokens are in `src/app/globals.css`; they follow shadcn's variable names, so shadcn components pick them up.

- **Flat.** Fog ground, hairline rules, 2px corners, no shadows, no gradients.
- **One loud colour.** Bill red means a person is needed: failures, disabled endpoints, the one destructive action.
- **Status is a shape before it is a colour.** A filled dot is succeeded, a cross is failed, a ring is pending (`StatusBadge`). It must read in greyscale.
- **Machine text is mono.** Ids, times, codes and durations use the `data` class (Spline Sans Mono, tabular figures); labels use `eyebrow`.
- **A masthead, not a sidebar.** Sections are text tabs.
- **44px targets.** Buttons and fields are 44px tall; 36px only inside dense rows.

Text colours were checked on fog: ink 15.4:1, graphite 6.8, wing 5.1, bill 5.5, sea 5.6, ochre 6.2.

## Adding a feature

Create `src/features/<name>/` with the files above, add its route under `src/app/(dashboard)/`, and add its tab to `src/app/(dashboard)/nav.ts`. Add shadcn components with `pnpm dlx shadcn@latest add <component>`, then restyle them to the tokens; they are source files we own.
