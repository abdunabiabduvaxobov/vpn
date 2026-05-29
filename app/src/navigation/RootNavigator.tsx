import React from 'react';
import {TouchableOpacity, Text, View, StyleSheet, Image} from 'react-native';
import {createNativeStackNavigator} from '@react-navigation/native-stack';
import {useTranslation} from 'react-i18next';
import {useNavigation} from '@react-navigation/native';
import type {NativeStackNavigationProp} from '@react-navigation/native-stack';

import {HomeScreen} from '../screens/HomeScreen';
import {ServerListScreen} from '../screens/ServerListScreen';
import {SettingsScreen} from '../screens/SettingsScreen';
import {AccountScreen} from '../screens/AccountScreen';
import {PaymentScreen} from '../screens/PaymentScreen';
import {SplitTunnelScreen} from '../screens/SplitTunnelScreen';
import {LoginScreen} from '../screens/LoginScreen';
import {useAuthStore} from '../stores/authStore';
import {colors, typography, spacing} from '../theme';

export type RootStackParamList = {
  Home: undefined;
  ServerList: undefined;
  Settings: undefined;
  Account: undefined;
  Payment: undefined;
  SplitTunnel: undefined;
  Login: undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();

const screenOptions = {
  headerStyle: {backgroundColor: colors.background},
  headerTintColor: colors.textPrimary,
  headerTitleStyle: typography.bodyBold,
  headerShadowVisible: false,
  contentStyle: {backgroundColor: colors.background},
};

export function RootNavigator() {
  const {t} = useTranslation();

  return (
    <Stack.Navigator screenOptions={screenOptions}>
      <Stack.Screen
        name="Home"
        component={HomeScreen}
        options={({navigation}) => ({
          headerTitle: '',
          headerLeft: () => <SettingsButton onPress={() => navigation.navigate('Settings')} />,
          headerRight: () => <ProfileButton onPress={() => navigation.navigate('Account')} />,
        })}
      />
      <Stack.Screen
        name="ServerList"
        component={ServerListScreen}
        options={{title: t('servers.title')}}
      />
      <Stack.Screen
        name="Settings"
        component={SettingsScreen}
        options={{title: t('settings.title')}}
      />
      <Stack.Screen
        name="Account"
        component={AccountScreen}
        options={{title: t('account.title')}}
      />
      <Stack.Screen
        name="Payment"
        component={PaymentScreen}
        options={{title: t('payment.title')}}
      />
      <Stack.Screen
        name="SplitTunnel"
        component={SplitTunnelScreen}
        options={{title: t('splitTunnel.title')}}
      />
      <Stack.Screen
        name="Login"
        component={LoginScreen}
        options={{title: t('login.title'), headerBackTitle: ''}}
      />
    </Stack.Navigator>
  );
}

function SettingsButton({onPress}: {onPress: () => void}) {
  return (
    <TouchableOpacity onPress={onPress} style={navStyles.headerBtn}>
      <View style={navStyles.settingsCircle}>
        <View style={navStyles.settingsInner}>
          <Text style={navStyles.settingsIcon}>{'⚙'}</Text>
        </View>
      </View>
    </TouchableOpacity>
  );
}

function ProfileButton({onPress}: {onPress: () => void}) {
  const user = useAuthStore(s => s.user);
  const initial = user?.full_name?.charAt(0)?.toUpperCase() || '?';

  return (
    <TouchableOpacity onPress={onPress} style={navStyles.headerBtn}>
      <View style={navStyles.avatarCircle}>
        <Text style={navStyles.avatarText}>{initial}</Text>
      </View>
    </TouchableOpacity>
  );
}

const navStyles = StyleSheet.create({
  headerBtn: {
    padding: spacing.sm,
  },
  settingsCircle: {
    width: 28,
    height: 28,
    borderRadius: 14,
    borderWidth: 1.5,
    borderColor: colors.textSecondary,
    justifyContent: 'center',
    alignItems: 'center',
  },
  settingsInner: {
    justifyContent: 'center',
    alignItems: 'center',
  },
  settingsIcon: {
    fontSize: 16,
    color: colors.textSecondary,
    lineHeight: 18,
  },
  avatarCircle: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: colors.primary,
    justifyContent: 'center',
    alignItems: 'center',
  },
  avatarText: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.textPrimary,
  },
});
