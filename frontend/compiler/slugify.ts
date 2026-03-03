/**
 * Convert heading text to a URL-friendly slug.
 * Lowercase, strip non-alphanumeric, collapse hyphens.
 */
export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    || "heading"
}
