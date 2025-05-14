from OpenSSL import crypto
import os


def generate_self_signed_cert(cert_file="cert.pem", key_file="key.pem"):
    k = crypto.PKey()
    k.generate_key(crypto.TYPE_RSA, 2048)

    cert = crypto.X509()
    cert.get_subject().C = "US"
    cert.get_subject().ST = "Region"
    cert.get_subject().L = "City"
    cert.get_subject().O = "Organization"
    cert.get_subject().OU = "Organizational Unit"
    cert.get_subject().CN = "localhost"
    cert.set_serial_number(1000)
    cert.gmtime_adj_notBefore(0)
    cert.gmtime_adj_notAfter(10 * 365 * 24 * 60 * 60)  # 10 years validity
    cert.set_issuer(cert.get_subject())
    cert.set_pubkey(k)
    cert.sign(k, "sha256")

    with open(cert_file, "wb") as cf:
        cf.write(crypto.dump_certificate(crypto.FILETYPE_PEM, cert))
    with open(key_file, "wb") as kf:
        kf.write(crypto.dump_privatekey(crypto.FILETYPE_PEM, k))

    print(f"Certificate generated: {cert_file}")
    print(f"Key file generated: {key_file}")


if __name__ == "__main__":
    generate_self_signed_cert()
