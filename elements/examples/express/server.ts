import express from 'express'
import cors from 'cors'
import { createExpressHandler } from '@gram-ai/elements/server/express'

const app = express()
const port = Number(process.env.PORT ?? 3001)

app.use(cors({ origin: 'http://localhost:3000' }))
app.use(express.json())

// Stub login endpoint
app.post('/api/login', (req, res) => {
  const { username, password } = req.body as {
    username: string
    password: string
  }
  if (!username || !password) {
    res.status(401).json({ error: 'Username and password are required' })
    return
  }
  const token = btoa(JSON.stringify({ username, timestamp: Date.now() }))
  res.json({ token })
})

// Chat session endpoint — uses the Express server adapter
app.post(
  '/api/chat/session',
  createExpressHandler({
    embedOrigin: process.env.EMBED_ORIGIN ?? 'http://localhost:3000',
    userIdentifier: 'user-123',
    expiresAfter: 3600,
  })
)

app.listen(port, () => {
  console.log(`Express server running on http://localhost:${port}`)
})
