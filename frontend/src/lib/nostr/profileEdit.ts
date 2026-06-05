import { signEvent } from './nostrLogin'
import { publishProfile, fetchProfile } from './pool'
import { auth, setProfile } from './auth.svelte'
import { setProfileCache } from './profiles.svelte'
import type { ProfileMetadata } from './types'

// The kind-0 fields the in-app editor manages. Other fields (banner, website, custom
// keys) are preserved untouched by merging onto the freshest known profile.
export type EditableProfile = Pick<
  ProfileMetadata,
  'display_name' | 'name' | 'about' | 'picture' | 'nip05' | 'lud16'
>

/**
 * Publishes the user's kind-0 profile with the given changes merged in. Starts from the
 * freshest profile (cached or re-fetched) so we never clobber fields the editor doesn't
 * show. Updates the local stores so the UI reflects it immediately.
 */
export async function publishMyProfile(changes: EditableProfile): Promise<void> {
  const me = auth.pubkey
  if (!me) throw new Error('Not signed in')
  const current = auth.profile ?? (await fetchProfile(me)) ?? {}
  const merged: ProfileMetadata = { ...current }
  // Apply changes; an empty string clears that field.
  for (const [k, v] of Object.entries(changes)) {
    const val = typeof v === 'string' ? v.trim() : v
    if (val) merged[k] = val
    else delete merged[k]
  }
  const signed = await signEvent({
    kind: 0,
    created_at: Math.floor(Date.now() / 1000),
    tags: [],
    content: JSON.stringify(merged),
  })
  await publishProfile(signed)
  setProfile(merged)
  setProfileCache(me, merged)
}
