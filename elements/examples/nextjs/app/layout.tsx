import './globals.css'

export const metadata = {
  title: 'Gram Elements — Next.js Example',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
