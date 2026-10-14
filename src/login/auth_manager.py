# src/auth_manager.py
import requests
import logging
from .. import constants

logger = logging.getLogger(__name__)

# Google Identity Platform Endpoints
V1_URL = "https://identitytoolkit.googleapis.com/v1/accounts"
V2_URL = "https://identitytoolkit.googleapis.com/v2/accounts"

class AuthManager:
    def __init__(self):
        self.id_token = None
        self.email = None

    def get_token(self):
        """Returns the current ID token for use in main.py or other modules."""
        return self.id_token

    def start_mfa_enrollment(self):
        """Standardized: Returns (bool, str, dict)"""
        url = f"{V2_URL}/mfaEnrollment:start?key={constants.FIREBASE_API_KEY}"
        payload = {"idToken": self.id_token, "totpEnrollmentInfo": {}}
        
        try:
            resp = requests.post(url, json=payload)
            data = resp.json()
            if resp.status_code == 200:
                info = data['totpSessionInfo']
                # Wrap the keys in a dictionary payload
                return True, constants.MSG.Auth.MFA_READY, {
                    "secret": info['sharedSecretKey'],
                    "session": info['sessionInfo']
                }

            return False, data.get('error', {}).get('message', constants.MSG.Auth.MFA_START_FAILED), {}
        except Exception as e:
            return False, str(e), {}

# src/auth_manager.py

    def sign_in(self, email, password):
        """Standardized: Returns (bool, str, dict)"""
        url = f"{V1_URL}:signInWithPassword?key={constants.FIREBASE_API_KEY}"
        payload = {"email": email, "password": password, "returnSecureToken": True}
        
        try:
            resp = requests.post(url, json=payload)
            data = resp.json()

            if resp.status_code == 200:
                # 1. CHECK FOR MFA CHALLENGE FIRST
                if "mfaPendingCredential" in data:
                    # Preserve email for after MFA verification (the MFA
                    # finalize response does not include an email field).
                    self.email = email

                    # NEW: Extract the enrollment ID from the mfaInfo list
                    mfa_info = data.get("mfaInfo", [])
                    enrollment_id = mfa_info[0].get("mfaEnrollmentId") if mfa_info else None

                    return False, constants.MSG.Auth.MFA_REQUIRED, {
                        "mfa_pending_cred": data.get("mfaPendingCredential"),
                        "mfa_enrollment_id": enrollment_id  # <--- Critical for v2 API
                    }
                
                # 2. PROCEED WITH NORMAL LOGIN
                self.id_token = data.get('idToken')
                self.email = data.get('email')
                
                if not self.id_token:
                    return False, constants.MSG.Auth.NO_TOKEN, {}

                return True, constants.MSG.Auth.SUCCESS, {"type": "login"}

            # Handle actual errors
            error_msg = data.get('error', {}).get('message', constants.MSG.Auth.LOGIN_FAILED)
            return False, error_msg, {}

        except Exception as e:
            return False, constants.MSG.Auth.INTERNAL_ERROR.format(e), {}
    

    def finalize_mfa_enrollment(self, session_info, user_code):
        """Standardized: Returns (bool, str, dict)"""
        url = f"{V2_URL}/mfaEnrollment:finalize?key={constants.FIREBASE_API_KEY}"
        payload = {
            "idToken": self.id_token,
            "displayName": "XALIVA TOTP",
            "totpVerificationInfo": {
                "sessionInfo": session_info,
                "verificationCode": user_code
            }
        }

        try:
            resp = requests.post(url, json=payload)
            data = resp.json()
            if resp.status_code == 200:
                self.id_token = data['idToken']
                return True, constants.MSG.Auth.MFA_ENROLLED, {"type": "mfa_finalize"}
            return False, constants.MSG.Auth.MFA_ENROLL_FAILED, {}
        except Exception as e:
            return False, constants.MSG.Auth.INTERNAL_ERROR.format(e), {}

    def send_verification_email(self):
        """Standardized: Returns (bool, str, dict)"""
        if not self.id_token: return False, constants.MSG.Auth.NO_SESSION, {}
        url = f"{V1_URL}:sendOobCode?key={constants.FIREBASE_API_KEY}"
        payload = {"requestType": "VERIFY_EMAIL", "idToken": self.id_token}
        try:
            resp = requests.post(url, json=payload)
            return (True, constants.MSG.Auth.EMAIL_SENT, {}) if resp.status_code == 200 else (False, constants.MSG.Auth.EMAIL_FAILED, {})
        except: return False, constants.MSG.Auth.EMAIL_CONN_ERROR, {}

    def is_email_verified(self):
        """Standardized: Returns (bool, str, dict)"""
        if not self.id_token: return False, constants.MSG.Auth.NO_SESSION, {"verified": False}
        url = f"{V1_URL}:lookup?key={constants.FIREBASE_API_KEY}"
        payload = {"idToken": self.id_token}
        try:
            resp = requests.post(url, json=payload)
            users = resp.json().get('users', [])
            verified = users[0].get('emailVerified', False) if users else False
            return True, "Status fetched", {"verified": verified}
        except Exception as e: 
            return False, str(e), {"verified": False}

    def sign_out(self):
        """Clears local session data."""
        self.id_token = None
        self.email = None

    def send_password_reset(self, email):
        """Sends a password reset email via Firebase. Returns (bool, str, dict)."""
        url = f"{V1_URL}:sendOobCode?key={constants.FIREBASE_API_KEY}"
        payload = {"requestType": "PASSWORD_RESET", "email": email}
        try:
            resp = requests.post(url, json=payload)
            data = resp.json()
            if resp.status_code == 200:
                return True, constants.MSG.Auth.RESET_EMAIL_SENT, {"email": data.get("email", email)}
            return False, data.get("error", {}).get("message", constants.MSG.Auth.RESET_FAILED), {}
        except Exception as e:
            return False, constants.MSG.Auth.INTERNAL_ERROR.format(e), {}

    # src/auth_manager.py

    def get_account_info(self):
        """Standardized: Checks if the user has enrolled in MFA. Returns (bool, str, dict)"""
        if not self.id_token:
            return False, constants.MSG.Auth.NO_SESSION, {"enrolled": False}
            
        url = f"{V1_URL}:lookup?key={constants.FIREBASE_API_KEY}"
        payload = {"idToken": self.id_token}
        
        try:
            resp = requests.post(url, json=payload)
            data = resp.json()
            if resp.status_code == 200:
                user_info = data.get('users', [{}])[0]
                # Look for 'mfaInfo' list. If it exists and has items, they are enrolled.
                mfa_enrollments = user_info.get('mfaInfo', [])
                is_enrolled = len(mfa_enrollments) > 0
                email_verified = user_info.get("emailVerified", False)
                return True, "Info fetched", {
                    "enrolled": is_enrolled,
                    "details": mfa_enrollments,
                    "email_verified": email_verified,
                }
            
            return False, data.get('error', {}).get('message', 'Lookup failed'), {"enrolled": False}
        except Exception as e:
            return False, str(e), {"enrolled": False}
        

    def verify_mfa_challenge(self, mfa_pending_credential, code, mfa_enrollment_id=None):
        """
        Completes a login using the v2 MFA Sign-In resource.
        Endpoint: POST /v2/accounts/mfaSignIn:finalize
        """
        url = f"https://identitytoolkit.googleapis.com/v2/accounts/mfaSignIn:finalize?key={constants.FIREBASE_API_KEY}"
        
        # --- FIX: Structure must match Google's nested format ---
        payload = {
            "mfaPendingCredential": mfa_pending_credential,
            "totpVerificationInfo": {
                "verificationCode": code
            }
        }
        
        # Add the ID if we have it
        if mfa_enrollment_id:
            payload["mfaEnrollmentId"] = mfa_enrollment_id
        
        # --- DEBUG LOGGING ---
        print(f"DEBUG: Posting to v2: {url}")
        # Print payload (Masking sensitive creds for security in logs)
        debug_payload = payload.copy()
        if "mfaPendingCredential" in debug_payload:
            debug_payload["mfaPendingCredential"] = "HIDDEN_CREDENTIAL_STRING"
        print(f"DEBUG: Payload Structure: {debug_payload}")
        
        resp = requests.post(url, json=payload)
        
        if resp.status_code != 200:
            try:
                # Try to get detailed error from Google
                err_json = resp.json()
                msg = err_json.get("error", {}).get("message", resp.text)
            except:
                msg = resp.text
            
            print(f"DEBUG: MFA Login Failed. Status: {resp.status_code}, Msg: {msg}")
            raise Exception(f"MFA Login Error: {msg}")

        # Success
        data = resp.json()
        self.id_token = data.get("idToken")
        self.refresh_token = data.get("refreshToken")
        # MFA finalize response may not include email; keep the one
        # saved during sign_in if the response omits it.
        self.email = data.get("email") or self.email

        logger.debug("Firebase id_token: %s", self.id_token)
        return data