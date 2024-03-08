# Unofficial Azure Managed Identity service
This is service mimics Azure Managed Identity service for non-Azure workloads and makes hybrid configurations simpler.
Client can simply call function `DefaultAzureCredential()` for when they want authenticate with Azure services, just like in Azure.

It was tested with Azure Key Vault but should works with other services too.

# Deployment
## Azure configuration
Create service principal with command `az ad sp create-for-rbac -n burmilla --years 10`

Configure these environment variables based on output of above command with cloud-init configuration like this:
```yaml
rancher:
  environment:
    AZIDENTITY_TENANTID: 00000000-0000-0000-0000-000000000000
    AZIDENTITY_CLIENTID: 11111111-1111-1111-1111-111111111111
    AZIDENTITY_SECRET: SecretValue
```

## Network configuration
Because Azure clients expect to find identity service from http://169.254.169.254 we need configure our installation to listening that IP address. It can be done with following cloud-init configurations.
### DHCP
```yaml
rancher:
  network:
    interfaces:
      eth0:
        dhcp: true
        post_up:
        - ip address add 169.254.169.254/32 dev eth0
```

## Static IP
```yaml
rancher:
  network:
    interfaces:
      eth0:
        addresses:
        - 10.10.10.100/24
        - 169.254.169.254/32
```

# Technical details
Implemented by catching Azure Key Vault identity requests with debug proxy.
That can be done by setting HTTP_PROXY and HTTPS_PROXY environment variables **but** you need build custom version which does not contain [this](https://github.com/Azure/azure-sdk-for-net/commit/be063672ae84cf79d18072fdae7a3e362b8d8be7) other why you cannot catch those request.

Sources:
* https://learn.microsoft.com/en-us/entra/identity/managed-identities-azure-resources/how-managed-identities-work-vm#system-assigned-managed-identity
* https://github.com/Azure/azure-sdk-for-net/tree/80c332520a63dad418d6e49ddd139858483b852b/sdk/identity/Azure.Identity#defaultazurecredential
* https://github.com/Azure/azure-sdk-for-net/blob/80c332520a63dad418d6e49ddd139858483b852b/sdk/mgmtcommon/AppAuthentication/Azure.Services.AppAuthentication/TokenProviders/MsiAccessTokenProvider.cs#L78-L81

## Request send by official client:
```bash
GET http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fvault.azure.net HTTP/1.1
Metadata: true
x-ms-client-request-id: 62152641-2531-4057-85eb-a0200fd885b2
x-ms-return-client-request-id: true
User-Agent: azsdk-net-Identity/1.11.0-alpha.20240307.1 (.NET Framework 4.8.4645.0; Microsoft Windows 10.0.20348 )
Host: 169.254.169.254
```

## Azure response:
```bash
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Server: IMDS/150.870.65.1103
Date: Thu, 07 Mar 2024 11:07:59 GMT
Content-Length: 1669

{
    "access_token": "<access token>",
    "client_id": "0d46c104-a85f-49e2-9c96-ae58c6fa927b",
    "expires_in": "86317",
    "expires_on": "1709895997",
    "ext_expires_in": "86399",
    "not_before": "1709809297",
    "resource": "https://vault.azure.net",
    "token_type": "Bearer"
}
```
