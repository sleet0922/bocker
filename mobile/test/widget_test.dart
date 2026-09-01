import 'package:bocker_mobile/main.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  testWidgets('登录页正常渲染', (tester) async {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    await tester.pumpWidget(const BockerMobileApp());
    await tester.pumpAndSettle();
    expect(find.text('Bocker 移动管理'), findsOneWidget);
    expect(find.text('服务器地址（IP 或域名）'), findsOneWidget);
    expect(find.text('用户名'), findsOneWidget);
    expect(find.text('密码'), findsOneWidget);
    expect(find.text('连接'), findsOneWidget);
  });
}
