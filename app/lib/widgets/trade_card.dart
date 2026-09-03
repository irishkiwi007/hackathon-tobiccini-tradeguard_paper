import 'package:flutter/material.dart';

import '../models/proposed_trade.dart';
import '../theme.dart';

class TradeCard extends StatelessWidget {
  final ProposedTrade trade;
  final VoidCallback onApprove;
  final VoidCallback onDeny;

  const TradeCard({
    super.key,
    required this.trade,
    required this.onApprove,
    required this.onDeny,
  });

  @override
  Widget build(BuildContext context) {
    // CALL/PUT is the badge now, not BUY — every trade this project
    // proposes is a buy (opening a long call or put), so a "BUY" badge
    // would be true but uninformative. Call = bullish = buy color, put =
    // bearish = sell color, matching the direction the trade expresses
    // even though the underlying Alpaca order side is always "buy".
    final directionColor = trade.isCall ? AppTheme.buy : AppTheme.sell;
    final directionLabel = trade.isCall ? 'CALL' : 'PUT';

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                  decoration: BoxDecoration(
                    color: directionColor.withOpacity(0.15),
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    directionLabel,
                    style: TextStyle(color: directionColor, fontWeight: FontWeight.bold),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    trade.underlying.isNotEmpty ? trade.underlying : trade.symbol,
                    style: Theme.of(context).textTheme.titleMedium,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                Text('x${trade.qty.toStringAsFixed(0)} ${trade.qty == 1 ? "contract" : "contracts"}'),
              ],
            ),
            if (trade.contractDescription.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                trade.contractDescription,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).textTheme.bodySmall?.color?.withOpacity(0.7),
                    ),
              ),
            ],
            const SizedBox(height: 12),

            // The plain-language reasoning is the whole point — this is
            // what a human actually approves or denies from.
            Text(trade.reasoning, style: Theme.of(context).textTheme.bodyMedium),

            // Bull/bear case — deliberately brief (the system prompt asks
            // for a sentence or two each), shown side by side so a human
            // can see at a glance that both sides were actually weighed,
            // not just read a conclusion. Each side lives in an Expanded,
            // not a bare Row child — that's the fix for the earlier
            // pixel-overflow bug in _RiskChip, applied here up front
            // rather than discovered the same way again.
            if (trade.bullCase.isNotEmpty || trade.bearCase.isNotEmpty) ...[
              const SizedBox(height: 10),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (trade.bullCase.isNotEmpty)
                    Expanded(
                      child: _CaseColumn(
                        label: 'Bull case',
                        text: trade.bullCase,
                        color: AppTheme.buy,
                      ),
                    ),
                  if (trade.bullCase.isNotEmpty && trade.bearCase.isNotEmpty)
                    const SizedBox(width: 12),
                  if (trade.bearCase.isNotEmpty)
                    Expanded(
                      child: _CaseColumn(
                        label: 'Bear case',
                        text: trade.bearCase,
                        color: AppTheme.sell,
                      ),
                    ),
                ],
              ),
            ],

            // A compact "sources" line — worth showing now that
            // internal/mcpclient.News (Go side) parses this down to just
            // headline/source/date instead of the raw MCP tool response.
            // That raw version (security envelope, full per-article
            // JSON, image URLs — confirmed via a real screenshot during
            // development) was unreadable and got deliberately hidden;
            // this clean version is worth surfacing again since it's a
            // genuine transparency feature — exactly which articles
            // informed the reasoning above — without cluttering the card.
            if (trade.newsContext.isNotEmpty) ...[
              const SizedBox(height: 10),
              Text(
                trade.newsContext,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).textTheme.bodySmall?.color?.withOpacity(0.65),
                      height: 1.4,
                    ),
                maxLines: 4,
                overflow: TextOverflow.ellipsis,
              ),
            ],

            if (trade.riskFlags.isNotEmpty) ...[
              const SizedBox(height: 12),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: trade.riskFlags
                    .map((f) => _RiskChip(
                          text: f,
                          color: f.startsWith('exceeds position size cap') ? AppTheme.sell : AppTheme.warn,
                        ))
                    .toList(),
              ),
            ],

            if (trade.exceedsPositionCap) ...[
              const SizedBox(height: 10),
              Row(
                children: [
                  const Icon(Icons.block, size: 14, color: AppTheme.sell),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      'Approve is disabled — this proposal exceeds the position size cap.',
                      style: const TextStyle(fontSize: 12, color: AppTheme.sell, fontWeight: FontWeight.w600),
                    ),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(onPressed: onDeny, child: const Text('Deny')),
                ),
                const SizedBox(width: 12),
                Expanded(
                  // Hard block, not just a warning chip: a proposal over
                  // the position size cap should never be one tap away
                  // from Approve. The executor enforces the same rule
                  // independently as a backstop (see execution.capExceeded)
                  // in case a trade ever reaches "approved" some other way,
                  // but the human shouldn't be able to get there from here.
                  child: FilledButton(
                    onPressed: trade.exceedsPositionCap ? null : onApprove,
                    child: Text(trade.exceedsPositionCap ? 'Exceeds cap' : 'Approve'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _CaseColumn extends StatelessWidget {
  final String label;
  final String text;
  final Color color;

  const _CaseColumn({required this.label, required this.text, required this.color});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: TextStyle(color: color, fontSize: 11, fontWeight: FontWeight.bold),
        ),
        const SizedBox(height: 2),
        Text(
          text,
          style: Theme.of(context).textTheme.bodySmall?.copyWith(height: 1.3),
          maxLines: 4,
          overflow: TextOverflow.ellipsis,
        ),
      ],
    );
  }
}

class _RiskChip extends StatelessWidget {
  final String text;
  final Color color;
  const _RiskChip({required this.text, this.color = AppTheme.warn});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: color.withOpacity(0.15),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.warning_amber_rounded, size: 14, color: color),
          const SizedBox(width: 4),
          // Flexible (not Expanded) is the fix here — without it, Text
          // lays out at its full natural single-line width with no
          // constraint to shrink or wrap against, which is exactly what
          // overflowed by well under a pixel on a long risk flag string
          // like the manually-inserted test trade's. Flexible lets it
          // wrap onto a second line instead when a chip's content is
          // close to the card's full width.
          Flexible(
            child: Text(text, style: TextStyle(fontSize: 12, color: color)),
          ),
        ],
      ),
    );
  }
}
