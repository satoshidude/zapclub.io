/**
 * Cloudflare Worker: YouTube audio URL extractor
 * Deploy to: yturl.zapclub.workers.dev (or workers.zapclub.io/yturl/)
 *
 * GET /?v=<videoId>
 * Returns: { "url": "https://...googlevideo.com/..." }
 * Error:   { "error": "..." }  with 4xx/5xx status
 */

const INNERTUBE_URL =
  'https://www.youtube.com/youtubei/v1/player?key=AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8'

// Clients tried in order; first one that returns a playable stream wins.
const CLIENTS = [
  {
    clientName: 'ANDROID_VR',
    clientVersion: '1.57.29',
    androidSdkVersion: 32,
    userAgent: 'com.google.android.apps.youtube.vr.oculus/1.57.29 (Linux; U; Android 12; Build/SKQ1.211006.001) gzip',
  },
  {
    clientName: 'ANDROID_MUSIC',
    clientVersion: '7.27.52',
    androidSdkVersion: 30,
    userAgent: 'com.google.android.apps.youtube.music/7.27.52 (Linux; U; Android 11) gzip',
  },
  {
    clientName: 'TV_EMBEDDED',
    clientVersion: '2.0',
    userAgent: 'Mozilla/5.0 (SMART-TV; Linux; Tizen 5.0) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/2.1 Chrome/56.0.2924.0 TV Safari/537.36',
    thirdParty: { embedUrl: 'https://www.youtube.com' },
  },
]

async function fetchAudioURL(videoId) {
  for (const client of CLIENTS) {
    const { userAgent, thirdParty, ...clientInfo } = client
    const body = {
      context: {
        client: clientInfo,
        ...(thirdParty ? { thirdParty } : {}),
      },
      videoId,
      contentCheckOk: true,
      racyCheckOk: true,
    }

    let resp
    try {
      resp = await fetch(INNERTUBE_URL, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'User-Agent': userAgent,
          'X-YouTube-Client-Name': String(clientInfo.clientName === 'TV_EMBEDDED' ? 85 : 67),
          'X-YouTube-Client-Version': clientInfo.clientVersion,
        },
        body: JSON.stringify(body),
      })
    } catch {
      continue
    }

    if (!resp.ok) continue

    let data
    try {
      data = await resp.json()
    } catch {
      continue
    }

    const status = data?.playabilityStatus?.status
    if (status !== 'OK') continue

    const formats = data?.streamingData?.adaptiveFormats || []
    // Prefer opus/webm audio; fallback to any audio
    const audio = formats
      .filter(f => f.mimeType?.startsWith('audio/'))
      .sort((a, b) => {
        const aOpus = a.mimeType?.includes('opus') ? 1 : 0
        const bOpus = b.mimeType?.includes('opus') ? 1 : 0
        if (bOpus !== aOpus) return bOpus - aOpus
        return (b.bitrate || 0) - (a.bitrate || 0)
      })

    if (audio.length > 0) {
      return audio[0].url
    }
  }
  return null
}

export default {
  async fetch(request) {
    if (request.method === 'OPTIONS') {
      return new Response(null, {
        headers: {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'GET',
        },
      })
    }

    const url = new URL(request.url)
    const videoId = url.searchParams.get('v')

    if (!videoId || !/^[\w-]{11}$/.test(videoId)) {
      return json({ error: 'missing or invalid ?v=' }, 400)
    }

    const audioURL = await fetchAudioURL(videoId)
    if (!audioURL) {
      return json({ error: 'could not extract audio URL from YouTube' }, 503)
    }

    return json({ url: audioURL })
  },
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
      'Cache-Control': 'no-store',
    },
  })
}
