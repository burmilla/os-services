$files = Get-ChildItem -Path ./d -Include "docker-*" -Exclude "docker-compose.yml" -Recurse
$filesShorted = $files | Sort-Object -Descending
$latestPublished = $filesShorted[0].Name -replace "docker-","" -replace ".yml",""

[array]$dockerVersionsToSkip = Get-Content ./.github/docker-versions-to-skip

Write-Host "Getting latest Docker release tag to publish"
$dockerReleases = Invoke-RestMethod -UseBasicParsing https://api.github.com/repos/moby/moby/releases
$nonPreviewReleases =  $dockerReleases | Where-Object {$_.prerelease -eq $false}
$versionsShorted = $nonPreviewReleases.name | Sort-Object -Descending
$latestMajor = ""
$latestToPublish = ""
forEach($v in $versionsShorted) {
  if ($latestMajor -eq "") {
    $latestMajor = ($v -split "\.")[0] -replace "^v",""
  }
  if ($v -like "v$($latestMajor).*") {
    continue
  }
  $latestToPublish = $v -replace "^v",""
  break
}

if ($latestToPublish -eq $latestPublished) {
  Write-Host "Version $latestToPublish is latest and already published"
  echo "CREATE_PR=false" >> $env:GITHUB_ENV
  return
}
if ($latestToPublish -in $dockerVersionsToSkip) {
  Write-Host "Version $latestToPublish is in skip list"
  echo "CREATE_PR=false" >> $env:GITHUB_ENV
  return
}
Write-Host "Version $latestToPublish is latest, trying to publish"    
try {
  $tarUrl = "https://download.docker.com/linux/static/stable/x86_64/docker-" + $latestToPublish + ".tgz"
  Invoke-RestMethod -Uri $tarUrl -Method HEAD
} catch {
  Write-Host "Package $tarUrl is not available"
  $latestToPublish | Out-File ./.github/docker-versions-to-skip -Append
  echo "CREATE_PR=true" >> $env:GITHUB_ENV
  echo "PR_TITLE=Skip Docker $latestToPublish" >> $env:GITHUB_ENV
  return
}

"- docker-$latestToPublish" | Out-File ./index.yml -Append

$dockerYML = @'
docker:
  image: ${REGISTRY_DOMAIN}/burmilla/os-docker:
'@

$dockerYML += $latestToPublish
$dockerYML += @'
${SUFFIX}
  command: ros user-docker
  environment:
  - HTTP_PROXY
  - HTTPS_PROXY
  - NO_PROXY
  labels:
    io.rancher.os.scope: system
    io.rancher.os.after: console
  net: host
  pid: host
  ipc: host
  uts: host
  privileged: true
  restart: always
  volumes_from:
  - all-volumes
  volumes:
  - /sys:/host/sys
  - /var/lib/system-docker:/var/lib/system-docker:shared

'@
$dockerYML | Out-File "./d/docker-$latestToPublish.yml" -Append
ln -s docker "./images/10-docker-$latestToPublish"

echo "CREATE_PR=true" >> $env:GITHUB_ENV
echo "PR_TITLE=Add Docker $latestToPublish" >> $env:GITHUB_ENV
