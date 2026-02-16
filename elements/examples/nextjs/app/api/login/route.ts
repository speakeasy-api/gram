import { NextResponse } from 'next/server'

export async function POST(request: Request) {
  const { username, password } = (await request.json()) as {
    username: string
    password: string
  }

  if (!username || !password) {
    return NextResponse.json(
      { error: 'Username and password are required' },
      { status: 401 }
    )
  }

  const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
  return NextResponse.json({ token })
}
