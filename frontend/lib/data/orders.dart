import 'package:flutter/foundation.dart';

import 'api.dart';
import 'seller.dart';

/// Where an order has got to, from the buyer's side. The same five stages the
/// backend keeps, so there is nothing to translate and nothing to get wrong.
enum OrderStatus { placed, accepted, rejected, picked, delivered }

extension OrderStatusInfo on OrderStatus {
  String get title => switch (this) {
    OrderStatus.placed => 'Waiting for the shop',
    OrderStatus.accepted => 'Accepted — being prepared',
    OrderStatus.rejected => 'Not accepted',
    OrderStatus.picked => 'On the way',
    OrderStatus.delivered => 'Delivered',
  };

  /// Nothing else is going to happen to these.
  bool get isOver =>
      this == OrderStatus.delivered || this == OrderStatus.rejected;
}

OrderStatus statusFrom(String? stage) => switch (stage) {
  'accepted' => OrderStatus.accepted,
  'rejected' => OrderStatus.rejected,
  'picked' => OrderStatus.picked,
  'delivered' => OrderStatus.delivered,
  _ => OrderStatus.placed,
};

/// Shared with the seller side, which speaks the same stage names.
OrderStage stageFrom(String? stage) => switch (stage) {
  'accepted' => OrderStage.accepted,
  'rejected' => OrderStage.rejected,
  'picked' => OrderStage.picked,
  'delivered' => OrderStage.delivered,
  _ => OrderStage.received,
};

/// One order as the person who placed it sees it.
/// How an order id is shown to a person.
///
/// The ids are `order-6` in the database, and the screens printed them raw —
/// "Order order-6" on the confirmation, "ORDER-6" in the list. That leaks a
/// storage detail and reads as a bug. A shopper quotes this number at
/// support, so it wants to look like a reference: `#0006`.
String orderRef(String id) {
  final digits = id.split('-').last;
  final n = int.tryParse(digits);
  return n == null ? '#$id' : '#${n.toString().padLeft(4, '0')}';
}

class MyOrder {
  final String id;
  final String itemTitle;
  final String storeName;
  final int units;
  final double amount;
  final DateTime placedAt;
  final OrderStatus status;
  final String rejectReason;
  final String address;

  /// The four digits to read out at the door. Empty unless the order is live —
  /// the backend only sends it between acceptance and delivery.
  final String deliveryCode;

  const MyOrder({
    required this.id,
    required this.itemTitle,
    required this.storeName,
    required this.units,
    required this.amount,
    required this.placedAt,
    required this.status,
    this.rejectReason = '',
    this.address = '',
    this.deliveryCode = '',
  });

  factory MyOrder.fromJson(Map<String, dynamic> r) => MyOrder(
    id: r['id'] as String,
    itemTitle: r['itemTitle'] as String? ?? '',
    storeName: r['storeName'] as String? ?? '',
    units: (r['units'] as num?)?.toInt() ?? 1,
    amount: (r['amount'] as num?)?.toDouble() ?? 0,
    placedAt:
        DateTime.tryParse(r['placedAt'] as String? ?? '') ?? DateTime.now(),
    status: statusFrom(r['stage'] as String?),
    rejectReason: r['rejectReason'] as String? ?? '',
    address: r['receiverAddress'] as String? ?? '',
    deliveryCode: r['deliveryCode'] as String? ?? '',
  );
}

/// The buyer's order history, straight from the server. There is no local
/// copy: an order that is not on the server did not happen.
class MyOrders extends ChangeNotifier {
  MyOrders._();
  static final MyOrders instance = MyOrders._();

  final List<MyOrder> _orders = [];
  bool _loading = false;
  String? _error;

  List<MyOrder> get orders => List.unmodifiable(_orders);
  bool get loading => _loading;
  String? get error => _error;

  /// Orders still in motion — the ones worth showing at the top.
  int get live => _orders.where((o) => !o.status.isOver).length;

  Future<void> load() async {
    _loading = true;
    notifyListeners();
    try {
      final fresh = await Api.instance.myOrders();
      _orders
        ..clear()
        ..addAll(fresh);
      _error = null;
    } catch (e) {
      logApiFailure('my orders', e);
      _error = 'Could not reach Unimiunte — try again in a moment.';
    }
    _loading = false;
    notifyListeners();
  }

  /// Commit the basket atomically. Failures propagate so the cart stays intact.
  Future<List<MyOrder>> place(
    List<({String itemId, int qty})> lines, {
    required String addressId,
    required String requestId,
    required double expectedTotal,
  }) async {
    final placed = await Api.instance.checkout(
      lines: lines,
      addressId: addressId,
      requestId: requestId,
      expectedTotal: expectedTotal,
    );
    await load();
    return placed;
  }

  void clear() {
    _orders.clear();
    notifyListeners();
  }
}
