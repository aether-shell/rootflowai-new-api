/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import { usageLogSchema } from '../../../data/schema'
import { DetailsDialog } from '../details-dialog'

const log = usageLogSchema.parse({
  id: 1,
  user_id: 2,
  created_at: 1,
  type: 5,
  content: 'temporarily unavailable',
  upstream_request_id: 'channel-request-secret',
  channel: 77,
  channel_name: 'vendor-secret',
  other: JSON.stringify({
    admin_info: {
      original_error: 'vendor balance exhausted',
      original_status_code: 402,
      original_error_type: 'openai_error',
      original_error_code: 'insufficient_balance',
      channel_type: 1,
      use_channel: ['77', '88'],
    },
  }),
})

describe('usage log details privacy', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      'Log Details': 'Log Details',
      'Channel Request ID': 'Channel Request ID',
      'Original Error (Admin Only)': 'Original Error (Admin Only)',
    })
  })

  test('shows channel diagnostics in the admin scope', () => {
    render(
      <DetailsDialog log={log} isAdmin open onOpenChange={() => undefined} />
    )

    expect(screen.getByText('channel-request-secret')).toBeInTheDocument()
    expect(screen.getByText('vendor balance exhausted')).toBeInTheDocument()
    expect(screen.getByText('insufficient_balance')).toBeInTheDocument()
  })

  test('does not render channel diagnostics outside the admin scope', () => {
    render(
      <DetailsDialog
        log={log}
        isAdmin={false}
        open
        onOpenChange={() => undefined}
      />
    )

    expect(screen.queryByText('channel-request-secret')).not.toBeInTheDocument()
    expect(
      screen.queryByText('vendor balance exhausted')
    ).not.toBeInTheDocument()
    expect(screen.queryByText('insufficient_balance')).not.toBeInTheDocument()
  })
})
