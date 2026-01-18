import { describe, expect, it } from 'vitest'
import { humanizeToolName } from './humanize'

describe('humanizeToolName', () => {
  describe('camelCase handling', () => {
    it('splits camelCase into separate words', () => {
      expect(humanizeToolName('getWeather')).toBe('Get Weather')
    })

    it('handles multiple camelCase boundaries', () => {
      expect(humanizeToolName('getCurrentUserProfile')).toBe(
        'Get Current User Profile'
      )
    })

    it('handles consecutive uppercase letters', () => {
      expect(humanizeToolName('getHTTPResponse')).toBe('Get H T T P Response')
    })
  })

  describe('separator handling', () => {
    it('replaces hyphens with spaces', () => {
      expect(humanizeToolName('get-weather')).toBe('Get Weather')
    })

    it('replaces underscores with spaces', () => {
      expect(humanizeToolName('get_weather')).toBe('Get Weather')
    })

    it('handles mixed separators', () => {
      expect(humanizeToolName('get-current_weather')).toBe('Get Current Weather')
    })

    it('handles multiple consecutive separators', () => {
      expect(humanizeToolName('get--weather')).toBe('Get Weather')
    })
  })

  describe('title case', () => {
    it('capitalizes first letter of each word', () => {
      expect(humanizeToolName('hello world')).toBe('Hello World')
    })

    it('splits all-uppercase on each letter (camelCase boundary)', () => {
      // All uppercase letters are treated as camelCase boundaries
      expect(humanizeToolName('HELLO')).toBe('H E L L O')
    })

    it('handles lowercase input correctly', () => {
      expect(humanizeToolName('hello')).toBe('Hello')
    })
  })

  describe('edge cases', () => {
    it('handles empty string', () => {
      expect(humanizeToolName('')).toBe('')
    })

    it('handles single word', () => {
      expect(humanizeToolName('weather')).toBe('Weather')
    })

    it('handles single character', () => {
      expect(humanizeToolName('a')).toBe('A')
    })

    it('handles already formatted string', () => {
      expect(humanizeToolName('Get Weather')).toBe('Get Weather')
    })
  })

  describe('real-world tool names', () => {
    it('handles snake_case tool names', () => {
      expect(humanizeToolName('send_email')).toBe('Send Email')
      expect(humanizeToolName('create_calendar_event')).toBe(
        'Create Calendar Event'
      )
    })

    it('handles kebab-case tool names', () => {
      expect(humanizeToolName('read-file')).toBe('Read File')
      expect(humanizeToolName('list-directory-contents')).toBe(
        'List Directory Contents'
      )
    })

    it('handles camelCase tool names', () => {
      expect(humanizeToolName('searchDocuments')).toBe('Search Documents')
      expect(humanizeToolName('executeQuery')).toBe('Execute Query')
    })
  })
})
