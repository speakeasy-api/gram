import './globals.css'

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <head>
        <title>Gram Elements — Next.js Example</title>
      </head>
      <body>{children}</body>
    </html>
  )
}
