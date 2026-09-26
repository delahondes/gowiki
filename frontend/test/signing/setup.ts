// Test setup for the signing suite.
//
// jsdom does not provide IndexedDB, so the keystore (which lives on IDB)
// would blow up on `indexedDB.open(...)`. `fake-indexeddb/auto` installs
// a spec-compliant in-memory polyfill on the global. It's local to this
// suite — the dialect corpus tests do not import it, so their setup
// stays untouched.
//
// The polyfill also resets between test files because Vitest reloads
// the module for each. Within a single test file, tests share IDB
// state; use unique usernames or beforeEach cleanup where that matters.

import "fake-indexeddb/auto"
