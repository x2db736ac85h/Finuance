import { describe, expect, it } from 'vitest';

import { ApiError, type Problem } from '../api/client';
import { errorMessage } from './errors';

const problem = (p: Partial<Problem>): Problem => ({
  type: '/problems/internal',
  title: 'Internal error',
  status: 500,
  code: 'internal',
  ...p,
});

describe('errorMessage', () => {
  it('adds the request id to a failure on the server’s side, so there is something to quote', () => {
    expect(errorMessage(new ApiError(problem({ requestId: 'cf0ff0a89c6f68dd' })))).toBe(
      'Internal error (Reference: cf0ff0a89c6f68dd)',
    );
    expect(
      errorMessage(
        new ApiError(problem({ status: 503, code: 'timeout', detail: 'The server is busy.', requestId: 'abc' })),
      ),
    ).toBe('The server is busy. (Reference: abc)');
  });
  it('keeps the message plain for what the person can fix, and when there is no id', () => {
    expect(
      errorMessage(
        new ApiError(
          problem({
            status: 422,
            code: 'validation_failed',
            title: 'Invalid',
            detail: 'The name is too long.',
            requestId: 'abc',
          }),
        ),
      ),
    ).toBe('The name is too long.');
    expect(errorMessage(new ApiError(problem({})))).toBe('Internal error');
  });
  it('explains a request that never reached the server', () => {
    expect(errorMessage(new TypeError('Failed to fetch'))).toMatch(/Can't reach the Finuance server/);
  });
});
