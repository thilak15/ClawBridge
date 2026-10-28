import os
import pytest
from clawbridge.vault.crypto import EnvelopeCipher, DecryptionError


def test_encrypt_decrypt_roundtrip():
    key = EnvelopeCipher.generate_key()
    cipher = EnvelopeCipher(key)
    plaintext = b"super-secret-slack-oauth-token-xoxb-12345"
    
    ciphertext = cipher.encrypt(plaintext)
    assert ciphertext != plaintext
    
    decrypted = cipher.decrypt(ciphertext)
    assert decrypted == plaintext


def test_different_nonces_produce_different_ciphertext():
    key = EnvelopeCipher.generate_key()
    cipher = EnvelopeCipher(key)
    plaintext = b"identical-secret"
    
    ct1 = cipher.encrypt(plaintext)
    ct2 = cipher.encrypt(plaintext)
    assert ct1 != ct2
    assert cipher.decrypt(ct1) == plaintext
    assert cipher.decrypt(ct2) == plaintext


def test_tampered_ciphertext_raises_decryption_error():
    key = EnvelopeCipher.generate_key()
    cipher = EnvelopeCipher(key)
    ciphertext = bytearray(cipher.encrypt(b"unaltered-token"))
    
    # Flip the last byte (corrupting auth tag or payload)
    ciphertext[-1] ^= 0xFF
    
    with pytest.raises(DecryptionError):
        cipher.decrypt(bytes(ciphertext))


def test_wrong_key_fails_to_decrypt():
    key1 = EnvelopeCipher.generate_key()
    key2 = EnvelopeCipher.generate_key()
    cipher1 = EnvelopeCipher(key1)
    cipher2 = EnvelopeCipher(key2)
    
    ciphertext = cipher1.encrypt(b"token-for-cipher1")
    with pytest.raises(DecryptionError):
        cipher2.decrypt(ciphertext)


def test_derive_key_from_passphrase():
    cipher1 = EnvelopeCipher.from_passphrase("master-vault-password-2026")
    ciphertext = cipher1.encrypt(b"passphrase-protected")
    
    cipher2 = EnvelopeCipher.from_passphrase("master-vault-password-2026")
    assert cipher2.decrypt(ciphertext) == b"passphrase-protected"
