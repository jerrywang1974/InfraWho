/** Best-effort clipboard write; returns false if unsupported or denied. */
export async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through
  }
  return false
}

/** Attempt to clear clipboard residual after copy (may fail without focus/permission). */
export async function clearClipboard(): Promise<boolean> {
  return writeClipboard('')
}
