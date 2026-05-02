const registry = {}

export function registerGame(def) {
  registry[def.slug] = def
}

export function getGame(slug) {
  return registry[slug] || {}
}
