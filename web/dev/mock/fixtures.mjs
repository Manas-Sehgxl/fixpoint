// Which reports exist in the mock, how long ago each was filed, and the
// phases each one went through. The last one is the live report: it plays in
// real time from queued to proven in about 40 seconds.

import * as S from './scenarios.mjs';

export const LIVE_ID = 'r_8d2wq7kfzm';

export function fixtures() {
  return [
    {
      id: 'r_1a7k3m9qxe', seq: 1, scn: S.staleTotal, agoMin: 196, scale: 4,
      phases: [{ intake: true }, { reproduce: 'failed' }, { attempt: 1, outcome: 'verified' }, { prove: true, testId: 't_1' }],
    },
    {
      id: 'r_2c9f4n2vwb', seq: 2, scn: S.couponCase, agoMin: 131, scale: 4, regressions: ['t_1'],
      phases: [{ intake: true }, { reproduce: 'failed' }, { attempt: 1, outcome: 'still_failing' }, { attempt: 2, outcome: 'verified' }, { prove: true, testId: 't_2' }],
    },
    {
      id: 'r_3h6t8p1zdu', seq: 3, scn: S.qtyBelowOne, agoMin: 94, scale: 4, regressions: ['t_1', 't_2'],
      phases: [{ intake: true }, { reproduce: 'failed' }, { attempt: 1, outcome: 'still_failing' }, { attempt: 2, outcome: 'still_failing' }, { attempt: 3, outcome: 'still_failing' }, { escalate: true }],
    },
    {
      id: 'r_4b2r5y8kcs', seq: 4, scn: S.addToCartKahwa, agoMin: 71, scale: 4,
      phases: [{ intake: true }, { reproduce: 'passed' }, { ask: S.addToCartKahwa.question }, { reply: S.addToCartKahwa.replies[0], after: 380 }, { reproduce: 'passed' }, { close: true, after: 1500 }],
      retryPhases: [{ retry: true }, { reproduce: 'passed' }, { ask: S.addToCartKahwa.question }],
    },
    {
      id: 'r_5e8u1w4jhn', seq: 5, scn: S.checkoutVague, agoMin: 41, scale: 4,
      phases: [{ intake: true }, { reproduce: 'passed' }, { ask: S.checkoutVague.question }, { reply: S.checkoutVague.replies[0], after: 640 }, { reproduce: 'passed' }, { ask: S.checkoutVague.followUp }],
      replyPhases: (text) => [{ reply: text }, { reproduce: 'passed' }, { close: true, after: 2 }],
    },
    {
      id: 'r_6m3x9a2gtp', seq: 6, scn: S.priceMismatch, agoMin: 23, scale: 4,
      phases: [{ intake: true }, { reproduce: 'error' }, { error: true }],
      retryPhases: [{ retry: true }, { reproduce: 'passed' }, { ask: S.priceMismatch.question }],
    },
    {
      id: 'r_7q5j2s6ylf', seq: 7, scn: S.searchCase, agoMin: 9, scale: 4, regressions: ['t_1', 't_2'],
      phases: [{ intake: true }, { reproduce: 'failed' }, { await: true }],
      approvePhases: [{ resume: true, redRun: 'run_1' }, { attempt: 1, outcome: 'verified' }, { prove: true, testId: 'next' }],
    },
    {
      id: LIVE_ID, seq: 8, scn: S.freeDelivery, live: true, scale: 1, regressions: ['t_1', 't_2'],
      phases: [{ intake: true, wait: 0.8 }, { reproduce: 'failed' }, { attempt: 1, outcome: 'verified' }, { prove: true, testId: 'next' }],
    },
  ];
}
