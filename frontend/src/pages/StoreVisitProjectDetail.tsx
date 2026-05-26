import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, ChevronDown, Download, Hand, ImagePlus, Play, Plus, RefreshCw, Save, Settings2, Sparkles, Trash2, UploadCloud, Wand2 } from "lucide-react";
import { toast } from "sonner";

import type { LLMStreamState, StoreVisitBloggerReference, StoreVisitDishGenerationItem, StoreVisitProject, StoreVisitSpot } from "@/types";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  if (!version) return trimmed;
  const suffix = encodeURIComponent(version);
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${suffix}`;
};

const extractDownloadFilename = (contentDisposition: string | undefined, fallback: string) => {
  if (!contentDisposition) return fallback;
  const utfMatch = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utfMatch?.[1]) {
    try {
      return decodeURIComponent(utfMatch[1]);
    } catch {
      return utfMatch[1];
    }
  }
  const plainMatch = contentDisposition.match(/filename="?([^"]+)"?/i);
  return plainMatch?.[1] || fallback;
};

const triggerBlobDownload = (blob: Blob, filename: string) => {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
};

type StoreVisitIntroTemplate = {
  key: string;
  label: string;
  description: string;
  content: string;
};

type StoreVisitDishMotionPreset = {
  key: string;
  label: string;
  description: string;
};

type DishGenerationSegmentDraft = {
  prompt: string;
  durationSeconds: string;
};

const storeVisitDishMotionPresets: StoreVisitDishMotionPreset[] = [
  { key: "cinematic_reveal", label: "电影感拉远", description: "更稳，适合大多数桌面展示。"},
  { key: "premium_tabletop", label: "精品桌面展示", description: "更像高级桌面广告，左右和高低 reveal 更均衡。"},
  { key: "luxury_orbit", label: "高级环绕广告", description: "更强调精品广告感和最后的 hero orbit。"},
  { key: "left_reveal", label: "左侧展开", description: "更偏向左侧打开桌面层次，适合展示摆盘关系。"},
  { key: "right_reveal", label: "右侧展开", description: "更偏向右侧打开桌面层次，适合左右漂移感。"},
  { key: "overhead_showcase", label: "俯视展示", description: "更强调轻微升高和接近俯拍的精品展示感。"},
  { key: "slow_pullback", label: "克制拉远", description: "最通用，像商业桌拍一样平稳拉远 reveal。"},
];

const getStoreVisitDishMotionPreset = (key?: string) =>
  storeVisitDishMotionPresets.find((preset) => preset.key === (key || "").trim()) || storeVisitDishMotionPresets[0];

const buildStoreVisitDishPromptDraft = (presetKey: string) => {
  switch (presetKey) {
    case "premium_tabletop":
      return [
        "Smooth cinematic drift to the left across the tabletop presentation, keeping the main subject as the hero of the frame with premium restaurant lighting and elegant shallow depth of field.",
        "Smooth cinematic drift to the right across the tabletop presentation, keeping the main subject as the hero of the frame with premium restaurant lighting and elegant shallow depth of field.",
        "Subtle upward reveal of the tabletop presentation, showing more of the arrangement while keeping the main subject visually rich and detailed.",
        "Subtle downward settle toward the tabletop presentation, keeping the main subject sharp, stable, and visually luxurious.",
        "Slow elegant orbit around the tabletop presentation, treating the main subject like a premium restaurant commercial hero shot.",
      ];
    case "luxury_orbit":
      return [
        "A luxury culinary commercial shot with the camera gliding left in a smooth, premium arc, while the tabletop presentation remains stable and refined.",
        "A luxury culinary commercial shot with the camera gliding right in a smooth, premium arc, while the tabletop presentation remains stable and refined.",
        "A luxury culinary commercial shot with a smooth upward reveal that opens the premium tabletop presentation and warm restaurant ambience.",
        "A luxury culinary commercial shot with a smooth downward refinement move that returns attention to the main subject on the table.",
        "A luxury culinary hero shot. The camera slowly circles around the tabletop presentation in a refined, controlled orbit while the subject remains detailed, glossy, and visually rich.",
      ];
    case "left_reveal":
      return [
        "A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back while drifting toward the left side of the tabletop presentation.",
        "A premium commercial tabletop shot. The camera continues revealing the tabletop with a gentle leftward cinematic drift, keeping the main subject visually dominant.",
        "A premium commercial tabletop shot. The camera subtly rises to show more of the tabletop layout from the left side while the subject remains stable.",
        "A premium commercial tabletop shot. The camera settles slightly downward toward the tabletop presentation, still favoring the left side reveal.",
        "A premium commercial tabletop shot. The camera finishes with a slow left-biased hero orbit around the tabletop presentation.",
      ];
    case "right_reveal":
      return [
        "A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back while drifting toward the right side of the tabletop presentation.",
        "A premium commercial tabletop shot. The camera continues revealing the tabletop with a gentle rightward cinematic drift, keeping the main subject visually dominant.",
        "A premium commercial tabletop shot. The camera subtly rises to show more of the tabletop layout from the right side while the subject remains stable.",
        "A premium commercial tabletop shot. The camera settles slightly downward toward the tabletop presentation, still favoring the right side reveal.",
        "A premium commercial tabletop shot. The camera finishes with a slow right-biased hero orbit around the tabletop presentation.",
      ];
    case "overhead_showcase":
      return [
        "A premium tabletop commercial shot. The camera starts close and smoothly lifts toward a higher angle, beginning to reveal more of the tabletop presentation.",
        "A premium tabletop commercial shot. The camera continues into a refined near-overhead reveal while the main subject remains crisp, stable, and visually rich.",
        "A premium tabletop commercial shot. The camera makes a subtle controlled overhead drift across the tabletop arrangement, showing more layout and atmosphere.",
        "A premium tabletop commercial shot. The camera gently lowers back toward the main subject while keeping the tabletop presentation elegant and stable.",
        "A premium tabletop hero shot. The camera ends with a slow overhead orbit around the tabletop presentation, maintaining a luxurious and cinematic mood.",
      ];
    case "slow_pullback":
      return [
        "A premium commercial tabletop shot. The camera begins close to the main subject and slowly pulls back in a smooth, controlled motion, revealing more of the tabletop.",
        "A premium commercial tabletop shot. The camera continues pulling back gently, showing more arrangement and atmosphere while keeping the main subject dominant.",
        "A premium commercial tabletop shot. The camera adds a subtle cinematic drift during the pullback, with warm lighting and refined tabletop mood.",
        "A premium commercial tabletop shot. The camera keeps pulling back with elegant stability, making the overall presentation feel premium and visually rich.",
        "A premium commercial tabletop shot. The camera ends with a refined wide tabletop reveal, keeping the main subject as the visual hero.",
      ];
    default:
      return [
        "A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back with a smooth cinematic drift toward the left, gradually revealing more of the tabletop presentation and surrounding atmosphere.",
        "A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back with a smooth cinematic drift toward the right, gradually revealing more of the tabletop presentation and surrounding atmosphere.",
        "A premium commercial tabletop shot. The camera slowly lifts into a slightly higher angle, revealing more of the tabletop presentation while keeping the main subject visually dominant.",
        "A premium commercial tabletop shot. The camera gently lowers toward the tabletop presentation in a smooth, refined motion while the main subject remains stable and visually dominant.",
        "A premium commercial tabletop shot. The camera performs a slow, elegant orbit around the main subject on the table, keeping the tabletop presentation stable, detailed, and visually rich.",
      ];
  }
};

const buildDishGenerationSegmentsForFrameCount = (
  frameCount: number,
  currentSegments: DishGenerationSegmentDraft[],
  presetKey: string,
): DishGenerationSegmentDraft[] => {
  const targetSegmentCount = Math.max(1, frameCount - 1);
  const presetPrompts = buildStoreVisitDishPromptDraft(presetKey);
  const next = currentSegments.slice(0, targetSegmentCount).map((segment) => ({
    prompt: segment.prompt,
    durationSeconds: segment.durationSeconds || "2",
  }));
  while (next.length < targetSegmentCount) {
    next.push({
      prompt: presetPrompts[next.length % presetPrompts.length],
      durationSeconds: "2",
    });
  }
  return next;
};

const getStoreVisitSpotType = (spot?: StoreVisitSpot | null) => (spot?.spot_type || "entrance").trim() || "entrance";

const getStoreVisitSpotLabel = (spot?: StoreVisitSpot | null) => {
  const name = (spot?.name || "").trim();
  if (name) return name;
  switch (getStoreVisitSpotType(spot)) {
    case "lobby":
      return "大厅";
    case "private_room":
      return "包间";
    case "kitchen":
      return "厨房";
    case "featured_area":
      return "特色区域";
    case "table_dishes":
      return "整桌菜品";
    case "dish_generation":
      return "菜品生成";
    case "signature_dish":
      return "招牌菜";
    case "taste_recommendation":
      return "口味推荐";
    case "promotion":
      return "优惠信息";
    default:
      return "门头";
  }
};

const getStoreVisitSpotFeelingHint = (spot?: StoreVisitSpot | null) => {
  switch (getStoreVisitSpotType(spot)) {
    case "lobby":
      return "大厅空间看起来舒适清楚，整体氛围适合坐下来慢慢吃。";
    case "private_room":
      return "包间给人的第一感觉是安静、私密，很适合聚餐。";
    case "kitchen":
      return "现场能明显感受到现做和烟火气，整体看起来干净利落。";
    case "featured_area":
      return "这个区域有明显记忆点，一眼就能看出这家店和别家的区别。";
    case "table_dishes":
      return "整桌摆出来以后看着丰富、真实，有让人想继续听下去的食欲感。";
    case "dish_generation":
      return "这里更适合直接用多张 key frame 做纯菜品视频，不再强塞主播进画面。";
    case "signature_dish":
      return "这道菜一眼就能看出记忆点，适合当成整家店的代表内容来介绍。";
    case "taste_recommendation":
      return "这部分重点是把口味感受和推荐理由讲清楚，让人听完就知道该不该点。";
    case "promotion":
      return "重点是把优惠和推荐理由说清楚，让人听完就知道值不值得马上来。";
    default:
      return "门头醒目，站在外面就会想进去试试。";
  }
};

const buildStoreVisitPromptGuidance = (spot?: StoreVisitSpot | null) => {
  const spotType = getStoreVisitSpotType(spot);
  const areaLabel = getStoreVisitSpotLabel(spot);
  if (spotType === "dish_generation") {
    return `当前${areaLabel}不走主播口播和 LLM 反推，而是直接上传任意数量的 key frame 图片，再按每段独立时长和 motion prompt 生成纯菜品视频。`;
  }
  if (spotType === "table_dishes" || spotType === "signature_dish" || spotType === "taste_recommendation") {
    return `系统会自动补全${areaLabel}视频里的人物姿态、坐姿和动作边界；你只需要写简短的介绍内容，表情、手势和说话节奏会交给 LLM 自行推理。`;
  }
  return `系统会自动补全${areaLabel}视频里的人物站姿和动作边界；你只需要写简短的介绍内容，表情、手势和说话节奏会交给 LLM 自行推理。`;
};

const buildStoreVisitDirectIntroTemplates = (spot?: StoreVisitSpot | null): StoreVisitIntroTemplate[] => {
  const feeling = getStoreVisitSpotFeelingHint(spot);
  const spotType = getStoreVisitSpotType(spot);
  switch (spotType) {
    case "dish_generation":
      return [
        {
          key: "ready-dish-generation",
          label: "成品：菜品视频",
          description: "这个区域不再写口播介绍，直接上传任意数量的 key frame 图来生成纯菜品视频。",
          content: `介绍内容：`,
        },
      ];
    case "lobby":
      return [
        {
          key: "ready-lobby-space",
          label: "成品：大厅环境",
          description: "适合直接测试大厅环境、空间感和整体氛围的口播反推效果。",
          content: `介绍内容：大厅看起来干净整洁，整体明亮通透，第一眼感觉很舒服。座位布局清楚，空间不拥挤，动线也比较顺，适合朋友聚餐或者日常随便来吃一顿。${feeling}`,
        },
        {
          key: "ready-lobby-comfort",
          label: "成品：大厅舒适度",
          description: "适合测试偏“坐着舒服、环境稳定”的大厅介绍。",
          content: `介绍内容：大厅整体不吵不乱，看起来比较舒展。桌椅摆放规整，灯光自然，坐下来不会觉得局促，比较适合家庭聚餐和朋友小聚。${feeling}`,
        },
      ];
    case "private_room":
      return [
        {
          key: "ready-room-private",
          label: "成品：包间私密",
          description: "适合直接测试包间的私密感、聚餐适配度和舒适度。",
          content: `介绍内容：这个包间空间独立，整体安静，私密感比较强。座位安排清楚，坐下来不会觉得压抑，适合朋友聚会、家庭聚餐或者稍微正式一点的饭局。${feeling}`,
        },
        {
          key: "ready-room-group",
          label: "成品：包间聚餐",
          description: "适合测试偏“适合多人聚餐”的包间介绍。",
          content: `介绍内容：这个包间看起来比较完整，坐多人也不会太挤。桌面和周围留白还可以，整体比较舒服，适合聚餐、庆生或者想安静聊天的时候来。${feeling}`,
        },
      ];
    case "kitchen":
      return [
        {
          key: "ready-kitchen-clean",
          label: "成品：厨房干净",
          description: "适合直接测试厨房或明档的干净程度和现做感。",
          content: `介绍内容：厨房区域看起来干净整洁，操作动线比较利落。能明显感受到现做和出餐节奏，不是那种很乱的后厨感觉，既有烟火气，又不会让人觉得脏乱。${feeling}`,
        },
        {
          key: "ready-kitchen-open",
          label: "成品：明档现做",
          description: "适合测试偏“明档可视、现做安心”的厨房介绍。",
          content: `介绍内容：这个区域一看就知道是现做现出的感觉。能看到操作和出餐过程，整体比较透明，有现场感，但看起来仍然整洁、有秩序。${feeling}`,
        },
      ];
    case "featured_area":
      return [
        {
          key: "ready-feature-memory",
          label: "成品：特色记忆点",
          description: "适合直接测试特色区域最值得拍、最有辨识度的介绍。",
          content: `介绍内容：这里是这家店最有记忆点的区域，一眼就能看出这家店的风格和别的店不太一样。如果是第一次来，这里很容易留下印象。${feeling}`,
        },
        {
          key: "ready-feature-checkin",
          label: "成品：打卡区域",
          description: "适合测试偏“好拍、好记、值得打卡”的特色区域介绍。",
          content: `介绍内容：这个区域本身就很适合停下来拍一下，视觉上很集中，也容易被记住。来这家店的时候，很适合顺手打卡。${feeling}`,
        },
      ];
    case "table_dishes":
      return [
        {
          key: "ready-table-overview",
          label: "成品：整桌菜品",
          description: "适合直接测试一整桌菜摆在面前时的丰富度、卖相和食欲感介绍。",
          content: `介绍内容：这一桌看起来菜品很丰富，摆上来之后第一眼就挺有食欲。荤素搭配比较完整，颜色层次也比较清楚，看起来不单调，第一次来直接照着这一桌的思路点会比较稳。${feeling}`,
        },
        {
          key: "ready-table-combo",
          label: "成品：一桌好点",
          description: "适合测试偏“这桌怎么点更合适”的整桌介绍。",
          content: `介绍内容：这一桌搭配看起来比较完整，既有主菜也有辅助配菜。桌面层次很清楚，菜品放在一起看着就比较有满足感，如果是两三个人来吃，照着这个方向点会比较稳。${feeling}`,
        },
      ];
    case "signature_dish":
      return [
        {
          key: "ready-signature-dish",
          label: "成品：招牌菜",
          description: "适合直接测试一道招牌菜为什么值得点、为什么容易被记住。",
          content: `介绍内容：这道菜是这家店比较有代表性的招牌内容，端上来之后视觉上很抓人，一眼就能注意到。不管是第一次来还是想点代表菜，这道都很值得提一下。${feeling}`,
        },
        {
          key: "ready-signature-memory",
          label: "成品：招牌记忆点",
          description: "适合测试偏“这道菜是整家店记忆点”的介绍。",
          content: `介绍内容：这道菜是很多人来这里会优先想到的一道，颜色、摆盘或者分量都很容易让人记住。如果只能先推荐一道菜，这道很适合先说。${feeling}`,
        },
      ];
    case "taste_recommendation":
      return [
        {
          key: "ready-taste-reco",
          label: "成品：口味推荐",
          description: "适合直接测试口味特点、适合谁吃以及点单建议。",
          content: `介绍内容：整体口味比较稳定，接受度高，吃起来不会很突兀。适合第一次来的人，也适合平时想稳一点点单的人；如果不知道怎么选，可以先从这类味型或这几道开始。${feeling}`,
        },
        {
          key: "ready-taste-safe",
          label: "成品：稳妥推荐",
          description: "适合测试偏“怎么点更稳、不容易踩雷”的口味介绍。",
          content: `介绍内容：味道走向比较清楚，不会让人吃不懂。适合第一次来试，也适合带朋友一起点；如果想点得稳一点，可以优先参考这类口味和搭配。${feeling}`,
        },
      ];
    case "promotion":
      return [
        {
          key: "ready-promo-offer",
          label: "成品：优惠推荐",
          description: "适合测试最后收尾时讲优惠、套餐和性价比。",
          content: `介绍内容：这里最近有值得顺手薅的优惠或套餐，可以直接提一套最划算、最省心的点法。整体更适合第一次来的人，或者想省心点单的人，最后直接给一个明确结论，告诉大家值不值得来。`,
        },
        {
          key: "ready-promo-value",
          label: "成品：收尾种草",
          description: "适合测试偏“讲完一圈后最后给推荐结论”的收尾口播。",
          content: `介绍内容：这里现在有比较容易入手的团购、套餐或活动，重点可以提一个最有性价比的选择。更适合日常吃饭、聚会或者第一次来试试的人，最后直接说清楚值不值得来、适不适合收藏。`,
        },
      ];
    default:
      return [
        {
          key: "ready-food",
          label: "成品：门头开场",
          description: `适合直接测试当前${getStoreVisitSpotLabel(spot)}的门头开场口播。`,
          content: `介绍内容：宝子们，我发现了一家一眼就会注意到的店。门头很醒目，整体看起来接地气又好找，第一感觉就会想进去看看。${feeling}`,
        },
        {
          key: "ready-new-opening",
          label: "成品：新店发现",
          description: `适合测试“路过被吸引、第一次介绍${getStoreVisitSpotLabel(spot)}”的说法。`,
          content: `介绍内容：宝子们，我刚路过就被这家店门头吸引住了。整体看起来干净明亮，第一眼就挺有记忆点，我带你们一起进去看看。`,
        },
        {
          key: "ready-value",
          label: "成品：性价比推荐",
          description: `适合测试“值不值得来、适不适合顺手打卡”的${getStoreVisitSpotLabel(spot)}介绍。`,
          content: `介绍内容：宝子们，这家店我先给结论，属于顺路看到可以放心进来的类型。门头不花哨，但整体很直给，也挺有生活气息。`,
        },
      ];
  }
};

const buildStoreVisitEditableIntroTemplates = (spot?: StoreVisitSpot | null): StoreVisitIntroTemplate[] => {
  const areaLabel = getStoreVisitSpotLabel(spot);
  const spotType = getStoreVisitSpotType(spot);
  switch (spotType) {
    case "dish_generation":
      return [
        {
          key: "summary-dish-generation",
          label: "骨架：菜品生成",
          description: `当前${areaLabel}不需要介绍内容，直接新增一组 key frame 图片即可。`,
          content: `介绍内容：`,
        },
      ];
    case "lobby":
      return [
        {
          key: "summary-lobby",
          label: "骨架：大厅环境",
          description: `适合写${areaLabel}的空间感、整洁度、氛围和适合什么人来。`,
          content: `介绍内容：`,
        },
      ];
    case "private_room":
      return [
        {
          key: "summary-room",
          label: "骨架：包间私密",
          description: `适合写${areaLabel}的私密感、舒适度和适合什么场景。`,
          content: `介绍内容：`,
        },
      ];
    case "kitchen":
      return [
        {
          key: "summary-kitchen",
          label: "骨架：厨房现做",
          description: `适合写${areaLabel}的干净程度、现做感和烟火气。`,
          content: `介绍内容：`,
        },
      ];
    case "featured_area":
      return [
        {
          key: "summary-feature",
          label: "骨架：特色记忆点",
          description: `适合写${areaLabel}最值得拍、最能体现门店气质的内容。`,
          content: `介绍内容：`,
        },
      ];
    case "table_dishes":
      return [
        {
          key: "summary-table-dishes",
          label: "骨架：整桌菜品",
          description: `适合写${areaLabel}的丰富度、桌面层次、整体卖相和适合怎么点。`,
          content: `介绍内容：`,
        },
      ];
    case "signature_dish":
      return [
        {
          key: "summary-signature-dish",
          label: "骨架：招牌菜",
          description: `适合写${areaLabel}的卖相、记忆点和为什么值得点。`,
          content: `介绍内容：`,
        },
      ];
    case "taste_recommendation":
      return [
        {
          key: "summary-taste-recommendation",
          label: "骨架：口味推荐",
          description: `适合写${areaLabel}的口味特点、适合谁吃和点单建议。`,
          content: `介绍内容：`,
        },
      ];
    case "promotion":
      return [
        {
          key: "summary-promotion",
          label: "骨架：优惠收尾",
          description: `适合写${areaLabel}里的优惠、套餐和最后的推荐结论。`,
          content: `介绍内容：`,
        },
      ];
    default:
      return [
        {
          key: "summary-basic",
          label: "骨架：门头简版",
          description: `最适合当前链路，直接写${areaLabel}的介绍内容。`,
          content: `介绍内容：`,
        },
      ];
  }
};

interface BackgroundTaskRecord {
  id: string;
  status: "pending" | "running" | "completed" | "failed";
  progress: number;
  result?: string;
  error?: string;
}

type MissingReferenceSpot = {
  spot_id: number;
  spot_type: string;
  name: string;
};

type StoreVisitProjectGenerateMode = "full" | "prompts";
type StoreVisitProjectResetMode = "images" | "videos" | "all";

const defaultStoreVisitAutoGenerateTemplate = `店铺整体介绍：
门店最吸引人的点：
推荐内容：
优惠信息：
补充要求：`;

export default function StoreVisitProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [project, setProject] = useState<StoreVisitProject | null>(null);
  const [bloggerReferences, setBloggerReferences] = useState<StoreVisitBloggerReference[]>([]);
  const [spots, setSpots] = useState<StoreVisitSpot[]>([]);
  const [dishGenerationItems, setDishGenerationItems] = useState<StoreVisitDishGenerationItem[]>([]);
  const [selectedSpotId, setSelectedSpotId] = useState<number | null>(null);
  const [savingContent, setSavingContent] = useState(false);
  const [runningImage, setRunningImage] = useState(false);
  const [runningVideo, setRunningVideo] = useState(false);
  const [rerollingImage, setRerollingImage] = useState(false);
  const [rerollingVideo, setRerollingVideo] = useState(false);
  const [resettingState, setResettingState] = useState(false);
  const [interruptingGeneration, setInterruptingGeneration] = useState(false);
  const [selectingBloggerReferenceId, setSelectingBloggerReferenceId] = useState<number | null>(null);
  const [promptDialogOpen, setPromptDialogOpen] = useState(false);
  const [resolutionDialogOpen, setResolutionDialogOpen] = useState(false);
  const [projectGenerateDialogOpen, setProjectGenerateDialogOpen] = useState(false);
  const [projectGenerateProgressOpen, setProjectGenerateProgressOpen] = useState(false);
  const [projectResetDialogOpen, setProjectResetDialogOpen] = useState(false);
  const [projectResetMode, setProjectResetMode] = useState<StoreVisitProjectResetMode | null>(null);
  const [dishGenerationDialogOpen, setDishGenerationDialogOpen] = useState(false);
  const [creatingDishGenerationItem, setCreatingDishGenerationItem] = useState(false);
  const [runningDishGenerationItemId, setRunningDishGenerationItemId] = useState<number | null>(null);
  const [resettingDishGenerationItemId, setResettingDishGenerationItemId] = useState<number | null>(null);
  const [interruptingDishGenerationItemId, setInterruptingDishGenerationItemId] = useState<number | null>(null);
  const [deletingDishGenerationItemId, setDeletingDishGenerationItemId] = useState<number | null>(null);
  const [editingDishGenerationItemId, setEditingDishGenerationItemId] = useState<number | null>(null);
  const [dishGenerationPresetKey, setDishGenerationPresetKey] = useState(storeVisitDishMotionPresets[0].key);
  const [dishGenerationExistingFrames, setDishGenerationExistingFrames] = useState<string[]>([]);
  const [dishGenerationFrameFiles, setDishGenerationFrameFiles] = useState<(File | null)[]>([null, null]);
  const [dishGenerationSegments, setDishGenerationSegments] = useState<DishGenerationSegmentDraft[]>(
    buildStoreVisitDishPromptDraft(storeVisitDishMotionPresets[0].key).slice(0, 1).map((prompt) => ({
      prompt,
      durationSeconds: "2",
    })),
  );
  const [previewState, setPreviewState] = useState<{ kind: "image" | "video"; title: string; url: string; version?: string } | null>(null);
  const [selectedIntroTemplateKey, setSelectedIntroTemplateKey] = useState("ready-food");
  const [projectGenerateContent, setProjectGenerateContent] = useState("");
  const [projectGenerateSubmitting, setProjectGenerateSubmitting] = useState(false);
  const [projectGenerateMode, setProjectGenerateMode] = useState<StoreVisitProjectGenerateMode>("full");
  const [projectGenerateTaskId, setProjectGenerateTaskId] = useState("");
  const [projectGenerateTask, setProjectGenerateTask] = useState<BackgroundTaskRecord | null>(null);
  const [projectGenerateTaskStream, setProjectGenerateTaskStream] = useState<LLMStreamState | null>(null);
  const [projectBatchTaskId, setProjectBatchTaskId] = useState("");
  const [projectBatchTask, setProjectBatchTask] = useState<BackgroundTaskRecord | null>(null);
  const [projectBatchAction, setProjectBatchAction] = useState<"images" | "videos" | null>(null);
  const [projectBatchSubmitting, setProjectBatchSubmitting] = useState(false);
  const [resettingProjectMode, setResettingProjectMode] = useState<StoreVisitProjectResetMode | null>(null);
  const [projectGenerateRequestError, setProjectGenerateRequestError] = useState("");
  const [projectGenerateMissingRefs, setProjectGenerateMissingRefs] = useState<MissingReferenceSpot[]>([]);
  const [projectGenerateMissingDishGeneration, setProjectGenerateMissingDishGeneration] = useState(false);
  const [exportingArchive, setExportingArchive] = useState(false);
  const [exportingMerged, setExportingMerged] = useState(false);
  const [refreshingPage, setRefreshingPage] = useState(false);
  const [savingReferenceImage, setSavingReferenceImage] = useState(false);
  const [clearingReferenceImage, setClearingReferenceImage] = useState(false);
  const [exportMenuOpen, setExportMenuOpen] = useState(false);
  const [projectGenerateMenuOpen, setProjectGenerateMenuOpen] = useState(false);
  const [projectBatchMenuOpen, setProjectBatchMenuOpen] = useState(false);
  const [projectResetMenuOpen, setProjectResetMenuOpen] = useState(false);
  const projectGenerateHandledRef = useRef(false);
  const projectBatchHandledRef = useRef(false);
  const projectGenerateStreamRef = useRef<HTMLDivElement | null>(null);

  const [introText, setIntroText] = useState("");
  const [referenceImage, setReferenceImage] = useState<File | null>(null);
  const referenceInputRef = useRef<HTMLInputElement | null>(null);

  const [imagePositivePrompt, setImagePositivePrompt] = useState("");
  const [imageNegativePrompt, setImageNegativePrompt] = useState("");
  const [videoPositivePrompt, setVideoPositivePrompt] = useState("");
  const [videoNegativePrompt, setVideoNegativePrompt] = useState("");
  const [videoDurationSeconds, setVideoDurationSeconds] = useState("10");
  const [videoWidth, setVideoWidth] = useState("720");
  const [videoHeight, setVideoHeight] = useState("1280");

  const spot = useMemo(
    () => spots.find((item) => item.id === selectedSpotId) || spots[0] || null,
    [selectedSpotId, spots],
  );
  const spotType = getStoreVisitSpotType(spot);
  const isDishGenerationSpot = spotType === "dish_generation";
  const spotLabel = getStoreVisitSpotLabel(spot);
  const storeVisitPromptGuidance = useMemo(() => buildStoreVisitPromptGuidance(spot), [spot]);
  const storeVisitDirectIntroTemplates = useMemo(() => buildStoreVisitDirectIntroTemplates(spot), [spot]);
  const storeVisitEditableIntroTemplates = useMemo(() => buildStoreVisitEditableIntroTemplates(spot), [spot]);
  const storeVisitIntroTemplates = useMemo(
    () => [...storeVisitDirectIntroTemplates, ...storeVisitEditableIntroTemplates],
    [storeVisitDirectIntroTemplates, storeVisitEditableIntroTemplates],
  );

  const selectedReferencePreview = useMemo(() => {
    if (!referenceImage) return "";
    return URL.createObjectURL(referenceImage);
  }, [referenceImage]);

  useEffect(() => {
    return () => {
      if (selectedReferencePreview) {
        URL.revokeObjectURL(selectedReferencePreview);
      }
    };
  }, [selectedReferencePreview]);

  const syncSpotDrafts = (nextSpot: StoreVisitSpot | null) => {
    setIntroText(nextSpot?.intro_text?.trim() || buildStoreVisitDirectIntroTemplates(nextSpot)[0].content);
    setImagePositivePrompt(nextSpot?.image_positive_prompt || "");
    setImageNegativePrompt(nextSpot?.image_negative_prompt || "");
    setVideoPositivePrompt(nextSpot?.video_positive_prompt || "");
    setVideoNegativePrompt(nextSpot?.video_negative_prompt || "");
    setVideoDurationSeconds(`${nextSpot?.video_duration_seconds || 10}`);
    setVideoWidth(`${nextSpot?.video_width || 720}`);
    setVideoHeight(`${nextSpot?.video_height || 1280}`);
    setReferenceImage(null);
    if (referenceInputRef.current) {
      referenceInputRef.current.value = "";
    }
  };

  const fetchProject = async () => {
    if (!id) return;
    const res = await axios.get(`/api/store-visits/${id}`);
    setProject(res.data);
  };

  const fetchBloggerReferences = async () => {
    if (!id) return;
    const res = await axios.get(`/api/store-visits/${id}/blogger-references`);
    setBloggerReferences(res.data || []);
  };

  const fetchSpots = async () => {
    if (!id) return;
    const res = await axios.get(`/api/store-visits/${id}/spots`);
    const nextSpots = res.data as StoreVisitSpot[];
    setSpots(nextSpots);
    const nextSelectedSpot =
      nextSpots.find((item) => item.id === selectedSpotId) || nextSpots[0] || null;
    setSelectedSpotId(nextSelectedSpot?.id || null);
    syncSpotDrafts(nextSelectedSpot);
  };

  const fetchDishGenerationItems = async (spotId?: number | null) => {
    if (!spotId) {
      setDishGenerationItems([]);
      return;
    }
    const res = await axios.get(`/api/store-visit-spots/${spotId}/dish-generation-items`);
    setDishGenerationItems(res.data || []);
  };

  const refreshAll = async () => {
    if (!id) return;
    try {
      await Promise.all([fetchProject(), fetchSpots(), fetchBloggerReferences()]);
    } catch (err) {
      console.error(err);
      toast.error("获取博主探店详情失败");
    }
  };

  useEffect(() => {
    refreshAll();
  }, [id]);

  useEffect(() => {
    setProjectGenerateContent((prev) => {
      if (prev.trim()) return prev;
      return (project?.auto_generate_content || "").trim() || defaultStoreVisitAutoGenerateTemplate;
    });
  }, [project?.auto_generate_content]);

  useEffect(() => {
    if (!spot || !isDishGenerationSpot) {
      setDishGenerationItems([]);
      return;
    }
    fetchDishGenerationItems(spot.id).catch((err) => {
      console.error(err);
      toast.error("获取菜品生成条目失败");
    });
  }, [spot?.id, isDishGenerationSpot]);

  useEffect(() => {
    if (!storeVisitIntroTemplates.some((item) => item.key === selectedIntroTemplateKey)) {
      setSelectedIntroTemplateKey(storeVisitIntroTemplates[0]?.key || "ready-food");
    }
  }, [selectedIntroTemplateKey, storeVisitIntroTemplates]);

  useEffect(() => {
    if (!spot) return;
    const dishGenerating = isDishGenerationSpot && dishGenerationItems.some((item) => item.video_status === "generating");
    if (spot.image_status !== "generating" && spot.video_status !== "generating" && !dishGenerating) return;
    const timer = window.setInterval(async () => {
      try {
        await Promise.all([
          fetchProject(),
          fetchSpots(),
          isDishGenerationSpot ? fetchDishGenerationItems(spot.id) : Promise.resolve(),
        ]);
      } catch (err) {
        console.error(err);
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [spot?.id, spot?.image_status, spot?.video_status, isDishGenerationSpot, dishGenerationItems]);

  useEffect(() => {
    if (!projectGenerateTaskId) return;

    let stopped = false;
    let taskTimer = 0;
    let streamTimer = 0;

    const stopPolling = () => {
      if (taskTimer) window.clearInterval(taskTimer);
      if (streamTimer) window.clearInterval(streamTimer);
    };

    const fetchTaskState = () => {
      axios
        .get(`/api/tasks/${projectGenerateTaskId}`)
        .then(async (res) => {
          if (stopped) return;
          const taskRecord = res.data as BackgroundTaskRecord;
          setProjectGenerateTask(taskRecord);

          if ((taskRecord.status === "completed" || taskRecord.status === "failed") && !projectGenerateHandledRef.current) {
            projectGenerateHandledRef.current = true;
            stopPolling();
            try {
              await Promise.all([fetchProject(), fetchSpots(), spot?.id ? fetchDishGenerationItems(spot.id) : Promise.resolve()]);
            } catch (err) {
              console.error(err);
            }
            if (taskRecord.status === "completed") {
              toast.success(projectGenerateMode === "prompts" ? "项目提示词已生成" : "项目一键生成完成");
            } else {
              toast.error(taskRecord.error || (projectGenerateMode === "prompts" ? "项目提示词生成失败" : "项目一键生成失败"));
            }
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    const fetchTaskStream = () => {
      axios
        .get(`/api/tasks/${projectGenerateTaskId}/llm-stream`)
        .then((res) => {
          if (!stopped) {
            setProjectGenerateTaskStream(res.data?.stream || null);
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    fetchTaskState();
    fetchTaskStream();

    taskTimer = window.setInterval(fetchTaskState, 1500);
    streamTimer = window.setInterval(fetchTaskStream, 700);

    return () => {
      stopped = true;
      stopPolling();
    };
  }, [projectGenerateTaskId, spot?.id]);

  useEffect(() => {
    if (!projectBatchTaskId) return;

    let stopped = false;
    let taskTimer = 0;

    const stopPolling = () => {
      if (taskTimer) window.clearInterval(taskTimer);
    };

    const fetchTaskState = () => {
      axios
        .get(`/api/tasks/${projectBatchTaskId}`)
        .then(async (res) => {
          if (stopped) return;
          const taskRecord = res.data as BackgroundTaskRecord;
          setProjectBatchTask(taskRecord);

          if ((taskRecord.status === "completed" || taskRecord.status === "failed") && !projectBatchHandledRef.current) {
            projectBatchHandledRef.current = true;
            stopPolling();
            try {
              await Promise.all([fetchProject(), fetchSpots(), spot?.id ? fetchDishGenerationItems(spot.id) : Promise.resolve()]);
            } catch (err) {
              console.error(err);
            }
            if (taskRecord.status === "completed") {
              toast.success(projectBatchAction === "images" ? "项目图片已批量生成" : "项目视频已批量生成");
            } else {
              toast.error(taskRecord.error || (projectBatchAction === "images" ? "项目图片批量生成失败" : "项目视频批量生成失败"));
            }
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    fetchTaskState();
    taskTimer = window.setInterval(fetchTaskState, 1500);

    return () => {
      stopped = true;
      stopPolling();
    };
  }, [projectBatchTaskId, projectBatchAction, spot?.id]);

  useEffect(() => {
    if (!projectGenerateProgressOpen) return;
    const streamEl = projectGenerateStreamRef.current;
    if (!streamEl) return;
    const rafId = window.requestAnimationFrame(() => {
      streamEl.scrollTop = streamEl.scrollHeight;
    });
    return () => window.cancelAnimationFrame(rafId);
  }, [projectGenerateProgressOpen, projectGenerateTaskStream?.content, projectGenerateTask?.status]);

  const saveSpotContent = async (showToast = true) => {
    if (!spot) return false;
    setSavingContent(true);
    try {
      const formData = new FormData();
      formData.append("intro_text", introText.trim());
      formData.append("video_width", `${Math.max(1, Number.parseInt(videoWidth, 10) || 720)}`);
      formData.append("video_height", `${Math.max(1, Number.parseInt(videoHeight, 10) || 1280)}`);
      const res = await axios.put(`/api/store-visit-spots/${spot.id}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setSpots((prev) => prev.map((item) => (item.id === spot.id ? res.data : item)));
      syncSpotDrafts(res.data);
      if (showToast) {
        toast.success(`${spotLabel}内容已保存`);
      }
      return true;
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `保存${spotLabel}内容失败`);
      return false;
    } finally {
      setSavingContent(false);
    }
  };

  const handleUploadSpotReferenceImage = async (file: File | null) => {
    if (!spot || !file) return;
    setReferenceImage(file);
    setSavingReferenceImage(true);
    try {
      const formData = new FormData();
      formData.append("reference_image", file);
      const res = await axios.put(`/api/store-visit-spots/${spot.id}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setSpots((prev) => prev.map((item) => (item.id === spot.id ? res.data : item)));
      syncSpotDrafts(res.data);
      toast.success(`${spotLabel}参考图已保存`);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `上传${spotLabel}参考图失败`);
    } finally {
      setSavingReferenceImage(false);
      setReferenceImage(null);
      if (referenceInputRef.current) {
        referenceInputRef.current.value = "";
      }
    }
  };

  const handleClearSpotReferenceImage = async () => {
    if (!spot) return;
    setClearingReferenceImage(true);
    try {
      const formData = new FormData();
      formData.append("clear_reference_image", "1");
      const res = await axios.put(`/api/store-visit-spots/${spot.id}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setSpots((prev) => prev.map((item) => (item.id === spot.id ? res.data : item)));
      syncSpotDrafts(res.data);
      toast.success(`${spotLabel}参考图已清空`);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `清空${spotLabel}参考图失败`);
    } finally {
      setClearingReferenceImage(false);
      setReferenceImage(null);
      if (referenceInputRef.current) {
        referenceInputRef.current.value = "";
      }
    }
  };

  const savePromptChanges = async () => {
    if (!spot) return;
    const duration = Number.parseInt(videoDurationSeconds, 10);
    const parsedWidth = Number.parseInt(videoWidth, 10);
    const parsedHeight = Number.parseInt(videoHeight, 10);
    if (!imagePositivePrompt.trim() || !imageNegativePrompt.trim() || !videoNegativePrompt.trim()) {
      toast.error("请填写完整的图片提示词和视频负向提示词");
      return;
    }
    if (!Number.isFinite(duration) || duration <= 0) {
      toast.error("视频时长必须大于 0");
      return;
    }
    if (!Number.isFinite(parsedWidth) || parsedWidth <= 0 || !Number.isFinite(parsedHeight) || parsedHeight <= 0) {
      toast.error("视频分辨率必须大于 0");
      return;
    }
    try {
      const formData = new FormData();
      formData.append("intro_text", introText.trim());
      formData.append("image_positive_prompt", imagePositivePrompt.trim());
      formData.append("image_negative_prompt", imageNegativePrompt.trim());
      formData.append("video_positive_prompt", videoPositivePrompt.trim());
      formData.append("video_negative_prompt", videoNegativePrompt.trim());
      formData.append("video_duration_seconds", `${duration}`);
      formData.append("video_width", `${parsedWidth}`);
      formData.append("video_height", `${parsedHeight}`);
      const res = await axios.put(`/api/store-visit-spots/${spot.id}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setSpots((prev) => prev.map((item) => (item.id === spot.id ? res.data : item)));
      syncSpotDrafts(res.data);
      setPromptDialogOpen(false);
      toast.success("提示词已更新");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存提示词失败");
    }
  };

  const handleGenerateImage = async () => {
    if (!spot) return;
    const saved = await saveSpotContent(false);
    if (!saved) return;
    setRunningImage(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/generate-image`);
      toast.success(`${spotLabel}图片生成任务已提交`);
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `提交${spotLabel}图片生成失败`);
    } finally {
      setRunningImage(false);
    }
  };

  const handleGenerateVideo = async () => {
    if (!spot) return;
    if (!videoPositivePrompt.trim()) {
      toast.error("请先填写视频提示词，或先使用项目级一键生成提示词");
      return;
    }
    const saved = await saveSpotContent(false);
    if (!saved) return;
    setRunningVideo(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/generate-video`);
      toast.success(`${spotLabel}视频生成任务已提交`);
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `提交${spotLabel}视频生成失败`);
    } finally {
      setRunningVideo(false);
    }
  };

  const handleRerollImage = async () => {
    if (!spot) return;
    const saved = await saveSpotContent(false);
    if (!saved) return;
    setRerollingImage(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/reroll-image`);
      toast.success(`${spotLabel}图片重新抽卡任务已提交`);
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `提交${spotLabel}图片重新抽卡失败`);
    } finally {
      setRerollingImage(false);
    }
  };

  const handleRerollVideo = async () => {
    if (!spot) return;
    if (!videoPositivePrompt.trim()) {
      toast.error("请先填写视频提示词，或先使用项目级一键生成提示词");
      return;
    }
    const saved = await saveSpotContent(false);
    if (!saved) return;
    setRerollingVideo(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/reroll-video`);
      toast.success(`${spotLabel}视频重新抽卡任务已提交`);
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `提交${spotLabel}视频重新抽卡失败`);
    } finally {
      setRerollingVideo(false);
    }
  };

  const handleResetState = async () => {
    if (!spot) return;
    setResettingState(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/reset-state`);
      toast.success(`${spotLabel}状态已重置`);
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || `重置${spotLabel}状态失败`);
    } finally {
      setResettingState(false);
    }
  };

  const handleInterruptGeneration = async () => {
    if (!spot) return;
    setInterruptingGeneration(true);
    try {
      await axios.post(`/api/store-visit-spots/${spot.id}/interrupt`);
      toast.success("已中断当前生成任务");
      await fetchSpots();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "中断当前生成任务失败");
    } finally {
      setInterruptingGeneration(false);
    }
  };

  const updateDishGenerationFrameFile = (index: number, file: File | null) => {
    setDishGenerationFrameFiles((prev) => prev.map((item, idx) => (idx === index ? file : item)));
  };

  const addDishGenerationFrameSlot = () => {
    const nextFrameFiles = [...dishGenerationFrameFiles, null];
    setDishGenerationFrameFiles(nextFrameFiles);
    setDishGenerationSegments(
      buildDishGenerationSegmentsForFrameCount(
        dishGenerationExistingFrames.length + nextFrameFiles.length,
        dishGenerationSegments,
        dishGenerationPresetKey,
      ),
    );
  };

  const removeDishGenerationFrameSlot = (_index: number) => {
    const minAppendSlots = editingDishGenerationItemId ? 0 : 2;
    const nextFrameFiles =
      dishGenerationFrameFiles.length <= minAppendSlots
        ? dishGenerationFrameFiles
        : dishGenerationFrameFiles.slice(0, -1);
    setDishGenerationFrameFiles(nextFrameFiles);
    setDishGenerationSegments(
      buildDishGenerationSegmentsForFrameCount(
        dishGenerationExistingFrames.length + nextFrameFiles.length,
        dishGenerationSegments,
        dishGenerationPresetKey,
      ),
    );
  };

  const handleDishGenerationPresetChange = (nextKey: string) => {
    setDishGenerationPresetKey(nextKey);
    setDishGenerationSegments((prev) =>
      buildDishGenerationSegmentsForFrameCount(
        dishGenerationExistingFrames.length + dishGenerationFrameFiles.length,
        prev.map((segment, index) => ({
          ...segment,
          prompt: buildStoreVisitDishPromptDraft(nextKey)[index % buildStoreVisitDishPromptDraft(nextKey).length],
        })),
        nextKey,
      ),
    );
  };

  const updateDishGenerationSegmentPrompt = (index: number, value: string) => {
    setDishGenerationSegments((prev) =>
      prev.map((segment, idx) => (idx === index ? { ...segment, prompt: value } : segment)),
    );
  };

  const updateDishGenerationSegmentDuration = (index: number, value: string) => {
    setDishGenerationSegments((prev) =>
      prev.map((segment, idx) => (idx === index ? { ...segment, durationSeconds: value } : segment)),
    );
  };

  const resetDishGenerationDraft = () => {
    setEditingDishGenerationItemId(null);
    setDishGenerationPresetKey(storeVisitDishMotionPresets[0].key);
    setDishGenerationExistingFrames([]);
    setDishGenerationFrameFiles([null, null]);
    setDishGenerationSegments(
      buildDishGenerationSegmentsForFrameCount(2, [], storeVisitDishMotionPresets[0].key),
    );
  };

  const openCreateDishGenerationDialog = () => {
    resetDishGenerationDraft();
    setDishGenerationDialogOpen(true);
  };

  const openEditDishGenerationDialog = (item: StoreVisitDishGenerationItem) => {
    setEditingDishGenerationItemId(item.id);
    setDishGenerationPresetKey(item.preset_key || storeVisitDishMotionPresets[0].key);
    setDishGenerationExistingFrames(item.frames || []);
    setDishGenerationFrameFiles([]);
    setDishGenerationSegments(
      item.segments.map((segment) => ({
        prompt: segment.prompt,
        durationSeconds: `${segment.duration_seconds || 2}`,
      })),
    );
    setDishGenerationDialogOpen(true);
  };

  const handleCreateDishGenerationItem = async () => {
    if (!spot) return;
    if (dishGenerationFrameFiles.length < 2 || dishGenerationFrameFiles.some((file) => !file)) {
      toast.error("请至少上传 2 张 key frame 图片，并补齐当前所有图片位");
      return;
    }
    if (dishGenerationSegments.length !== dishGenerationFrameFiles.length - 1) {
      toast.error("过渡段数量和图片数量不匹配");
      return;
    }
    if (dishGenerationSegments.some((segment) => !segment.prompt.trim())) {
      toast.error("请填写完整的每段提示词");
      return;
    }
    setCreatingDishGenerationItem(true);
    try {
      const formData = new FormData();
      formData.append("preset_key", dishGenerationPresetKey);
      formData.append(
        "segments_json",
        JSON.stringify(
          dishGenerationSegments.map((segment) => ({
            prompt: segment.prompt.trim(),
            duration_seconds: Math.max(0.1, Number.parseFloat(segment.durationSeconds) || 2),
          })),
        ),
      );
      dishGenerationFrameFiles.forEach((file) => {
        if (file) {
          formData.append("frame_images", file);
        }
      });
      await axios.post(`/api/store-visit-spots/${spot.id}/dish-generation-items`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      toast.success("菜品生成条目已新增");
      setDishGenerationDialogOpen(false);
      resetDishGenerationDraft();
      await fetchDishGenerationItems(spot.id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "新增菜品生成条目失败");
    } finally {
      setCreatingDishGenerationItem(false);
    }
  };

  const handleUpdateDishGenerationItem = async () => {
    if (!spot || !editingDishGenerationItemId) return;
    if (dishGenerationFrameFiles.some((file) => !file)) {
      toast.error("请补齐新增的 key frame 图片");
      return;
    }
    const totalFrameCount = dishGenerationExistingFrames.length + dishGenerationFrameFiles.length;
    if (totalFrameCount < 2) {
      toast.error("至少需要 2 张 key frame 图片");
      return;
    }
    if (dishGenerationSegments.length !== totalFrameCount - 1) {
      toast.error("过渡段数量和图片数量不匹配");
      return;
    }
    if (dishGenerationSegments.some((segment) => !segment.prompt.trim())) {
      toast.error("请填写完整的每段提示词");
      return;
    }
    setCreatingDishGenerationItem(true);
    try {
      const formData = new FormData();
      formData.append("preset_key", dishGenerationPresetKey);
      formData.append(
        "segments_json",
        JSON.stringify(
          dishGenerationSegments.map((segment) => ({
            prompt: segment.prompt.trim(),
            duration_seconds: Math.max(0.1, Number.parseFloat(segment.durationSeconds) || 2),
          })),
        ),
      );
      dishGenerationFrameFiles.forEach((file) => {
        if (file) {
          formData.append("frame_images", file);
        }
      });
      await axios.put(`/api/store-visit-dish-generation-items/${editingDishGenerationItemId}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      toast.success("菜品生成条目已更新");
      setDishGenerationDialogOpen(false);
      resetDishGenerationDraft();
      await fetchDishGenerationItems(spot.id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "更新菜品生成条目失败");
    } finally {
      setCreatingDishGenerationItem(false);
    }
  };

  const handleGenerateDishGenerationItem = async (item: StoreVisitDishGenerationItem) => {
    setRunningDishGenerationItemId(item.id);
    try {
      await axios.post(`/api/store-visit-dish-generation-items/${item.id}/generate-video`);
      toast.success("菜品生成任务已提交");
      await fetchDishGenerationItems(item.spot_id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交菜品生成任务失败");
    } finally {
      setRunningDishGenerationItemId(null);
    }
  };

  const handleResetDishGenerationItem = async (item: StoreVisitDishGenerationItem) => {
    setResettingDishGenerationItemId(item.id);
    try {
      await axios.post(`/api/store-visit-dish-generation-items/${item.id}/reset-state`);
      toast.success("菜品生成条目已重置");
      await fetchDishGenerationItems(item.spot_id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置菜品生成条目失败");
    } finally {
      setResettingDishGenerationItemId(null);
    }
  };

  const handleInterruptDishGenerationItem = async (item: StoreVisitDishGenerationItem) => {
    setInterruptingDishGenerationItemId(item.id);
    try {
      await axios.post(`/api/store-visit-dish-generation-items/${item.id}/interrupt`);
      toast.success("已中断当前生成任务");
      await fetchDishGenerationItems(item.spot_id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "中断菜品生成任务失败");
    } finally {
      setInterruptingDishGenerationItemId(null);
    }
  };

  const handleDeleteDishGenerationItem = async (item: StoreVisitDishGenerationItem) => {
    setDeletingDishGenerationItemId(item.id);
    try {
      await axios.delete(`/api/store-visit-dish-generation-items/${item.id}`);
      toast.success("菜品生成条目已删除");
      await fetchDishGenerationItems(item.spot_id);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除菜品生成条目失败");
    } finally {
      setDeletingDishGenerationItemId(null);
    }
  };

  const handleSelectBloggerReference = async (referenceId: number) => {
    if (!id || !project || selectingBloggerReferenceId === referenceId) return;
    setSelectingBloggerReferenceId(referenceId);
    try {
      const res = await axios.post(`/api/store-visits/${id}/blogger-references/${referenceId}/select`);
      setProject(res.data);
      await fetchBloggerReferences();
      toast.success("已切换当前博主参考图");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "切换博主参考图失败");
    } finally {
      setSelectingBloggerReferenceId(null);
    }
  };

  const submitProjectAutoGenerate = async (allowSkipMissingRefs = false) => {
    if (!id) return;
    const content = projectGenerateContent.trim();
    if (!content) {
      toast.error("请先填写项目总文案");
      return;
    }
    setProjectGenerateSubmitting(true);
    setProjectGenerateRequestError("");
    try {
      const res = await axios.post(`/api/store-visits/${id}/one-click-generate`, {
        content,
        allow_skip_missing_refs: allowSkipMissingRefs,
        prompts_only: projectGenerateMode === "prompts",
      });
      const nextTaskId = String(res.data?.task_id || "").trim();
      if (!nextTaskId) {
        throw new Error("未获取到一键生成任务 ID");
      }
      setProjectGenerateMissingRefs([]);
      setProjectGenerateMissingDishGeneration(false);
      setProjectGenerateRequestError("");
      setProjectGenerateDialogOpen(false);
      setProjectGenerateProgressOpen(true);
      projectGenerateHandledRef.current = false;
      setProjectGenerateTaskId(nextTaskId);
      setProjectGenerateTask({ id: nextTaskId, status: "pending", progress: 0 });
      setProjectGenerateTaskStream(null);
      setProject((prev) => (prev ? { ...prev, auto_generate_content: content } : prev));
    } catch (err: any) {
      console.error(err);
      if (err.response?.status === 409 && err.response?.data?.requires_confirmation) {
        setProjectGenerateMissingRefs(err.response?.data?.missing_reference_spots || []);
        setProjectGenerateMissingDishGeneration(Boolean(err.response?.data?.missing_dish_generation));
        return;
      }
      const message = err.response?.data?.error || err.message || "提交一键生成失败";
      setProjectGenerateRequestError(message);
      toast.error(message);
    } finally {
      setProjectGenerateSubmitting(false);
    }
  };

  const handleExportArchive = async () => {
    if (!id || !project) return;
    setExportingArchive(true);
    try {
      const res = await axios.post(`/api/store-visits/${id}/export`, {}, { responseType: "blob" });
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${project.code || "store_visit"}_export.zip`,
      );
      triggerBlobDownload(res.data, filename);
      toast.success("项目压缩包已导出");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "导出压缩包失败");
    } finally {
      setExportingArchive(false);
    }
  };

  const handleExportMerged = async () => {
    if (!id || !project) return;
    setExportingMerged(true);
    try {
      const res = await axios.post(`/api/store-visits/${id}/export-merged`, {}, { responseType: "blob" });
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${project.code || "store_visit"}_merged.mp4`,
      );
      triggerBlobDownload(res.data, filename);
      toast.success("项目合并视频已导出");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "导出合并视频失败");
    } finally {
      setExportingMerged(false);
    }
  };

  const handleRefreshPage = async () => {
    if (!id) return;
    setRefreshingPage(true);
    try {
      await Promise.all([
        fetchProject(),
        fetchSpots(),
        fetchBloggerReferences(),
        isDishGenerationSpot && spot?.id ? fetchDishGenerationItems(spot.id) : Promise.resolve(),
      ]);
      toast.success("页面内容已刷新");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "刷新页面内容失败");
    } finally {
      setRefreshingPage(false);
    }
  };

  const submitProjectBatchRender = async (kind: "images" | "videos") => {
    if (!id) return;
    setProjectBatchSubmitting(true);
    try {
      const endpoint = kind === "images" ? "generate-all-images" : "generate-all-videos";
      const res = await axios.post(`/api/store-visits/${id}/${endpoint}`);
      const nextTaskId = String(res.data?.task_id || "").trim();
      if (!nextTaskId) {
        throw new Error("未获取到批量生成任务 ID");
      }
      projectBatchHandledRef.current = false;
      setProjectBatchAction(kind);
      setProjectBatchTaskId(nextTaskId);
      setProjectBatchTask({
        id: nextTaskId,
        status: "pending",
        progress: 0,
      });
      toast.success(kind === "images" ? "批量图片生成任务已提交" : "批量视频生成任务已提交");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || (kind === "images" ? "提交批量图片生成失败" : "提交批量视频生成失败"));
    } finally {
      setProjectBatchSubmitting(false);
    }
  };

  const handleProjectReset = async () => {
    if (!id || !projectResetMode) return;
    setResettingProjectMode(projectResetMode);
    try {
      const endpointMap: Record<StoreVisitProjectResetMode, string> = {
        images: "reset-all-images",
        videos: "reset-all-videos",
        all: "reset-all-states",
      };
      const successMap: Record<StoreVisitProjectResetMode, string> = {
        images: "项目图片状态已重置",
        videos: "项目视频状态已重置",
        all: "项目全部状态已重置",
      };
      await axios.post(`/api/store-visits/${id}/${endpointMap[projectResetMode]}`);
      setProjectBatchTaskId("");
      setProjectBatchTask(null);
      setProjectBatchAction(null);
      projectBatchHandledRef.current = false;
      toast.success(successMap[projectResetMode]);
      setProjectResetDialogOpen(false);
      setProjectResetMode(null);
      await Promise.all([fetchProject(), fetchSpots(), spot?.id ? fetchDishGenerationItems(spot.id) : Promise.resolve()]);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置项目状态失败");
    } finally {
      setResettingProjectMode(null);
    }
  };

  const projectGenerateTaskActive =
    !!projectGenerateTaskId &&
    (!projectGenerateTask || projectGenerateTask.status === "pending" || projectGenerateTask.status === "running");
  const projectBatchTaskActive =
    !!projectBatchTaskId &&
    (!projectBatchTask || projectBatchTask.status === "pending" || projectBatchTask.status === "running");

  const controlsDisabled =
    !spot || spot.image_status === "generating" || spot.video_status === "generating" || projectGenerateTaskActive;
  const imageReady = !!spot?.generated_image;

  const openProjectGenerateDialog = (mode: StoreVisitProjectGenerateMode) => {
    setProjectGenerateMenuOpen(false);
    setProjectGenerateMode(mode);
    setProjectGenerateRequestError("");
    setProjectGenerateMissingRefs([]);
    setProjectGenerateMissingDishGeneration(false);
    setProjectGenerateDialogOpen(true);
  };

  const openProjectResetDialog = (mode: StoreVisitProjectResetMode) => {
    setProjectResetMenuOpen(false);
    setProjectResetMode(mode);
    setProjectResetDialogOpen(true);
  };
  const videoReady = !!spot?.generated_video;
  const isGenerating = !!spot && (spot.image_status === "generating" || spot.video_status === "generating");
  const selectedIntroTemplate = useMemo(
    () => storeVisitIntroTemplates.find((item) => item.key === selectedIntroTemplateKey) || storeVisitIntroTemplates[0],
    [selectedIntroTemplateKey, storeVisitIntroTemplates],
  );

  return (
    <div className="space-y-6">
      <button
        onClick={() => navigate("/store-visits")}
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="w-4 h-4" />
        返回博主探店项目列表
      </button>

      <div className="flex items-start justify-between gap-4">
        <div className="space-y-2 min-w-0">
          <h1 className="text-3xl font-bold">{project?.name || "博主探店项目"}</h1>
          <p className="text-sm text-muted-foreground">
            {project?.description || ""}
            {project?.code ? ` · 文件夹：${project.code}` : ""}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => {
              void handleRefreshPage();
            }}
            disabled={!project || refreshingPage}
            className="inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
          >
            <RefreshCw className={`w-4 h-4 ${refreshingPage ? "animate-spin" : ""}`} />
            {refreshingPage ? "刷新中..." : "刷新"}
          </button>
          <Popover open={exportMenuOpen} onOpenChange={setExportMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={!project || exportingArchive || exportingMerged}
                className="inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                <Download className="w-4 h-4" />
                {exportingArchive || exportingMerged ? "导出中..." : "导出"}
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <div className="space-y-1">
                <button
                  type="button"
                  onClick={() => {
                    setExportMenuOpen(false);
                    void handleExportArchive();
                  }}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  导出压缩包
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setExportMenuOpen(false);
                    void handleExportMerged();
                  }}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  导出合并视频
                </button>
              </div>
            </PopoverContent>
          </Popover>

          <Popover open={projectGenerateMenuOpen} onOpenChange={setProjectGenerateMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={!project || projectGenerateTaskActive}
                className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
              >
                <Sparkles className="w-4 h-4" />
                {projectGenerateTaskActive ? "生成中..." : "一键生成"}
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-60 p-2">
              <div className="space-y-1">
                <button
                  type="button"
                  onClick={() => openProjectGenerateDialog("prompts")}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键生成提示词
                </button>
                <button
                  type="button"
                  onClick={() => openProjectGenerateDialog("full")}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键生成
                </button>
              </div>
            </PopoverContent>
          </Popover>

          <Popover open={projectBatchMenuOpen} onOpenChange={setProjectBatchMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={!project || projectBatchSubmitting || !!resettingProjectMode}
                className="inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                {projectBatchTaskActive && projectBatchAction === "images" ? <ImagePlus className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                {projectBatchTaskActive ? "批量生成中..." : "一键批量生成"}
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-64 p-2">
              <div className="space-y-1">
                <button
                  type="button"
                  onClick={() => {
                    setProjectBatchMenuOpen(false);
                    void submitProjectBatchRender("images");
                  }}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键生成所有图片
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setProjectBatchMenuOpen(false);
                    void submitProjectBatchRender("videos");
                  }}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键生成所有视频
                </button>
              </div>
            </PopoverContent>
          </Popover>

          <Popover open={projectResetMenuOpen} onOpenChange={setProjectResetMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={!project || !!resettingProjectMode}
                className="inline-flex items-center gap-2 rounded-md border border-destructive/30 px-4 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
              >
                <Trash2 className="w-4 h-4" />
                {resettingProjectMode ? "重置中..." : "一键重置"}
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <div className="space-y-1">
                <button
                  type="button"
                  onClick={() => openProjectResetDialog("images")}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键重置图片
                </button>
                <button
                  type="button"
                  onClick={() => openProjectResetDialog("videos")}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键重置视频
                </button>
                <button
                  type="button"
                  onClick={() => openProjectResetDialog("all")}
                  className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                >
                  一键重置所有
                </button>
              </div>
            </PopoverContent>
          </Popover>
        </div>
      </div>

      {spots.length > 0 && (
        <div className="rounded-2xl border border-border bg-card p-3">
          <div className="flex flex-wrap gap-2">
            {spots.map((item) => {
              const active = item.id === spot?.id;
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => {
                    setSelectedSpotId(item.id);
                    syncSpotDrafts(item);
                  }}
                  className={`rounded-full border px-3 py-2 text-sm transition-colors ${
                    active
                      ? "border-primary bg-primary/10 text-primary"
                      : "border-border text-muted-foreground hover:bg-accent"
                  }`}
                >
                  <span>{getStoreVisitSpotLabel(item)}</span>
                  <span className="ml-2 text-[11px] opacity-80">
                    图 {item.image_status || "draft"} / 视 {item.video_status || "draft"}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-[260px_1fr]">
        <div className="rounded-2xl border border-border bg-card p-4 space-y-4">
          {!isDishGenerationSpot ? (
            <>
              <div>
                <div className="text-sm font-medium mb-2">博主参考图</div>
                <div className="aspect-[4/5] max-h-[260px] rounded-xl bg-muted/30 border border-border/60 overflow-hidden flex items-center justify-center">
                  {project?.blogger_reference_image ? (
                    <img
                      src={withAssetVersion(project.blogger_reference_image, project.updated_at)}
                      alt={project.name}
                      className="w-full h-full object-contain"
                    />
                  ) : (
                    <div className="text-sm text-muted-foreground">暂无参考图</div>
                  )}
                </div>
              </div>

              {bloggerReferences.length > 0 && (
                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">点击切换当前使用的人物参考图</div>
                  <div className="grid grid-cols-3 gap-2">
                    {bloggerReferences.map((reference, index) => {
                      const active = reference.id === (project?.selected_blogger_reference_id || 0);
                      return (
                        <button
                          key={reference.id}
                          type="button"
                          disabled={controlsDisabled || selectingBloggerReferenceId === reference.id}
                          onClick={() => handleSelectBloggerReference(reference.id)}
                          className={`space-y-1 rounded-xl border p-1 text-left transition-colors disabled:opacity-50 ${
                            active ? "border-primary bg-primary/5" : "border-border hover:bg-accent"
                          }`}
                        >
                          <div className="aspect-[4/5] overflow-hidden rounded-lg bg-muted/20">
                            <img
                              src={withAssetVersion(reference.image_path, reference.updated_at)}
                              alt={`博主参考图 ${index + 1}`}
                              className="h-full w-full object-cover"
                            />
                          </div>
                          <div className={`truncate px-1 text-[10px] ${active ? "text-primary" : "text-muted-foreground"}`}>
                            {active ? `当前使用 · ${index + 1}` : `候选 ${index + 1}`}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="rounded-2xl border border-border bg-muted/10 p-4 space-y-3">
              <div className="text-sm font-medium">菜品生成工作流</div>
              <div className="text-sm text-muted-foreground leading-6">
                当前区域不使用博主参考图，也不走 LLM 反推。
                <br />
                你只需要为每一条上传任意数量的 key frame 图片，再按每段独立时长和 motion prompt 直接生成视频。
              </div>
            </div>
          )}

          {spot && (
            <>
              <div className="text-xs text-muted-foreground leading-6">
                当前{spotLabel}图片工作流：<span className="font-medium text-foreground">b_qwen_Image_edit_subgraphed.json</span>
                <br />
                当前{spotLabel}视频工作流：<span className="font-medium text-foreground">video_new_ltx2_3_i2v.json</span>
                <br />
                视频分辨率固定：<span className="font-medium text-foreground">720 × 1280</span>
              </div>
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div className="rounded-xl border border-border bg-muted/20 p-3">
                  <div className="text-muted-foreground text-xs">图片状态</div>
                  <div className="mt-1 font-medium">{spot.image_status || "draft"}</div>
                </div>
                <div className="rounded-xl border border-border bg-muted/20 p-3">
                <div className="text-muted-foreground text-xs">视频状态</div>
                  <div className="mt-1 font-medium">{spot.video_status || "draft"}</div>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setResolutionDialogOpen(true)}
                disabled={controlsDisabled}
                className="inline-flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                <Settings2 className="w-4 h-4" />
                修改视频分辨率（{videoWidth} × {videoHeight}）
              </button>
            </>
          )}
        </div>

        {spot && isDishGenerationSpot && (
          <div className="rounded-2xl border border-border bg-card p-4 space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0 flex-1">
                <h2 className="text-xl font-semibold">{spotLabel}</h2>
                <p className="text-sm text-muted-foreground mt-1">每一行上传任意数量的 key frame 图片，按逐段时长和逐段 prompt 生成纯菜品视频。</p>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={openCreateDishGenerationDialog}
                  className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                >
                  <Plus className="w-4 h-4" />
                  新增一组
                </button>
              </div>
            </div>

            <div className="rounded-2xl border border-border bg-muted/10 p-4 text-sm text-muted-foreground leading-6">
              当前会按你设置的视频参数覆盖工作流：
              <span className="mx-2 font-medium text-foreground">{videoWidth} × {videoHeight}</span>
              <span className="font-medium text-foreground">25 fps</span>
              <br />
              每一条菜品生成会按单段时长单独生成：
              <span className="mx-2 font-medium text-foreground">默认每段 2 秒</span>
              <span className="font-medium text-foreground">总计约 10 秒</span>
              <br />
              当前工作流：
              <span className="ml-2 font-medium text-foreground">dynamic tabletop keyframes</span>
            </div>

            {dishGenerationItems.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border bg-muted/10 p-10 text-center text-sm text-muted-foreground">
                还没有菜品生成条目，先新增一组 key frame 图片试试。
              </div>
            ) : (
              <div className="space-y-4">
                {dishGenerationItems.map((item) => {
                  const preset = storeVisitDishMotionPresets.find((entry) => entry.key === item.preset_key) || storeVisitDishMotionPresets[0];
                  const frameImages = item.frames || [];
                  const rowBusy = item.video_status === "generating";
                  const totalDuration = (item.segments || []).reduce((sum, segment) => sum + (segment.duration_seconds || 0), 0);
                  return (
                    <div key={item.id} className="rounded-2xl border border-border bg-muted/10 p-4 space-y-4">
                      <div className="flex items-start justify-between gap-4">
                        <div>
                          <div className="text-base font-semibold">第 {item.sort_order} 组</div>
                          <div className="mt-1 text-sm text-muted-foreground">
                            当前预设：<span className="font-medium text-foreground">{preset.label}</span>
                            <span className="ml-2">{preset.description}</span>
                          </div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            图片数：<span className="font-medium text-foreground">{frameImages.length}</span>
                            <span className="mx-2">·</span>
                            过渡段数：<span className="font-medium text-foreground">{item.segments.length}</span>
                            <span className="mx-2">·</span>
                            预计总时长：<span className="font-medium text-foreground">{totalDuration.toFixed(1)} 秒</span>
                          </div>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <button
                            type="button"
                            onClick={() => openEditDishGenerationDialog(item)}
                            disabled={rowBusy}
                            className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                          >
                            编辑参数
                          </button>
                          <button
                            type="button"
                            onClick={() => handleGenerateDishGenerationItem(item)}
                            disabled={rowBusy || runningDishGenerationItemId === item.id}
                            className="rounded-md bg-secondary px-3 py-2 text-sm text-secondary-foreground hover:bg-secondary/80 transition-colors disabled:opacity-50"
                          >
                            {runningDishGenerationItemId === item.id ? "生成中..." : "生成视频"}
                          </button>
                          <button
                            type="button"
                            onClick={() => handleInterruptDishGenerationItem(item)}
                            disabled={!rowBusy || interruptingDishGenerationItemId === item.id}
                            className="rounded-md border border-destructive/30 px-3 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
                          >
                            {interruptingDishGenerationItemId === item.id ? "中断中..." : "中断当前生成"}
                          </button>
                          <button
                            type="button"
                            onClick={() => handleResetDishGenerationItem(item)}
                            disabled={resettingDishGenerationItemId === item.id}
                            className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                          >
                            {resettingDishGenerationItemId === item.id ? "重置中..." : "重置状态"}
                          </button>
                          <button
                            type="button"
                            onClick={() => handleDeleteDishGenerationItem(item)}
                            disabled={rowBusy || deletingDishGenerationItemId === item.id}
                            className="inline-flex items-center gap-2 rounded-md border border-destructive/30 px-3 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
                          >
                            <Trash2 className="w-4 h-4" />
                            {deletingDishGenerationItemId === item.id ? "删除中..." : "删除"}
                          </button>
                        </div>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-6">
                        {frameImages.map((image, index) => (
                          <button
                            key={`${item.id}-${index}`}
                            type="button"
                            onClick={() =>
                              image &&
                              setPreviewState({
                                kind: "image",
                                title: `预览第 ${item.sort_order} 组 · Frame ${index + 1}`,
                                url: image,
                                version: item.updated_at,
                              })
                            }
                            className="rounded-xl border border-border bg-background p-2 text-left hover:bg-accent transition-colors"
                          >
                            <div className="aspect-square overflow-hidden rounded-lg bg-muted/20">
                              {image ? (
                                <img
                                  src={withAssetVersion(image, item.updated_at)}
                                  alt={`Frame ${index + 1}`}
                                  className="h-full w-full object-cover"
                                />
                              ) : (
                                <div className="flex h-full items-center justify-center text-xs text-muted-foreground">Frame {index + 1}</div>
                              )}
                            </div>
                            <div className="mt-2 text-xs text-muted-foreground">Frame {index + 1}</div>
                          </button>
                        ))}
                      </div>

                      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_260px]">
                        <div className="rounded-xl border border-border bg-background p-3 text-sm text-muted-foreground leading-6">
                          这组会按当前逐段配置依次生成并拼接成一个成片：
                          {(item.segments || []).map((segment, index) => (
                            <div key={`${item.id}-segment-${index}`} className="mt-2">
                              <span className="font-medium text-foreground">第 {index + 1} 段 · {segment.duration_seconds || 0} 秒</span>
                              <div className="mt-1 break-words text-xs leading-5">{segment.prompt}</div>
                            </div>
                          ))}
                        </div>
                        <button
                          type="button"
                          onClick={() =>
                            item.generated_video &&
                            setPreviewState({
                              kind: "video",
                              title: `预览第 ${item.sort_order} 组视频`,
                              url: item.generated_video,
                              version: item.updated_at,
                            })
                          }
                          disabled={!item.generated_video}
                          className="rounded-2xl border border-border bg-muted/10 p-3 text-left hover:bg-muted/20 transition-colors disabled:opacity-100 disabled:hover:bg-muted/10"
                        >
                          <div className="flex items-center justify-between">
                            <div className="text-sm font-medium">生成视频</div>
                            {item.video_generated_workflow && (
                              <div className="max-w-[120px] truncate text-[10px] text-muted-foreground">{item.video_generated_workflow}</div>
                            )}
                          </div>
                          <div className="mt-3 aspect-[9/16] max-h-[220px] rounded-xl overflow-hidden bg-muted/30 flex items-center justify-center">
                            {item.generated_video ? (
                              <video
                                src={withAssetVersion(item.generated_video, item.updated_at)}
                                className="w-full h-full object-contain"
                                muted
                                playsInline
                              />
                            ) : (
                              <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                <Play className="w-7 h-7" />
                                <span className="text-xs">{rowBusy ? "视频生成中..." : "还没有生成视频"}</span>
                              </div>
                            )}
                          </div>
                          <div className="mt-2 text-xs text-muted-foreground">
                            状态：{item.video_status || "draft"}{item.generated_video ? " · 点击预览" : ""}
                          </div>
                          {item.video_last_error && <div className="mt-2 text-xs text-destructive break-all">{item.video_last_error}</div>}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {spot && !isDishGenerationSpot && (
          <div className="rounded-2xl border border-border bg-card p-4 space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="min-w-0 flex-1">
                <h2 className="text-xl font-semibold">{spotLabel}</h2>
                <p className="text-sm text-muted-foreground mt-1">管理{spotLabel}参考图、{spotLabel}介绍，以及固定图片和视频提示词。</p>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2 min-w-[420px]">
                <button
                  onClick={() => setPromptDialogOpen(true)}
                  disabled={controlsDisabled}
                  className="flex items-center justify-center gap-2 rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                >
                  <Wand2 className="w-4 h-4" />
                  修改提示词
                </button>
                <button
                  onClick={handleResetState}
                  disabled={!spot || resettingState}
                  className="rounded-md border border-destructive/30 text-destructive px-3 py-2 text-sm hover:bg-destructive/10 transition-colors disabled:opacity-50"
                >
                  {resettingState ? "重置中..." : "重置状态"}
                </button>
                <button
                  onClick={handleInterruptGeneration}
                  disabled={!spot || !isGenerating || interruptingGeneration}
                  className="rounded-md border border-destructive/30 text-destructive px-3 py-2 text-sm hover:bg-destructive/10 transition-colors disabled:opacity-50"
                >
                  <span className="inline-flex items-center gap-2">
                    <Hand className="w-4 h-4" />
                    {interruptingGeneration ? "中断中..." : "中断当前生成"}
                  </span>
                </button>
              </div>
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
                <button
                  onClick={handleGenerateImage}
                  disabled={controlsDisabled || runningImage}
                  className="rounded-md bg-secondary text-secondary-foreground px-3 py-2 text-sm hover:bg-secondary/80 transition-colors disabled:opacity-50"
                >
                  {spot.image_status === "generating" ? "图片生成中..." : "生成图片"}
                </button>
                <button
                  onClick={handleGenerateVideo}
                  disabled={controlsDisabled || runningVideo || !spot.generated_image || !videoPositivePrompt.trim()}
                  className="rounded-md bg-secondary text-secondary-foreground px-3 py-2 text-sm hover:bg-secondary/80 transition-colors disabled:opacity-50"
                >
                  {spot.video_status === "generating" ? "视频生成中..." : "生成视频"}
                </button>
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
                <button
                  onClick={handleRerollImage}
                  disabled={!spot || controlsDisabled || rerollingImage}
                  className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                >
                  {rerollingImage ? "图片抽卡中..." : "重新抽卡（图片）"}
                </button>
                <button
                  onClick={handleRerollVideo}
                  disabled={!spot || controlsDisabled || rerollingVideo || !imageReady || !videoPositivePrompt.trim()}
                  className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                >
                  {rerollingVideo ? "视频抽卡中..." : "重新抽卡（视频）"}
                </button>
            </div>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_240px]">
              <div className="space-y-4">
                <div className="rounded-2xl border border-border bg-muted/10 p-4 space-y-4">
                  <div className="space-y-2">
                    <label className="text-sm font-medium">{spotLabel}参考图</label>
                    <input
                      ref={referenceInputRef}
                      type="file"
                      accept="image/*"
                      className="hidden"
                      onChange={(e) => handleUploadSpotReferenceImage(e.target.files?.[0] || null)}
                    />
                    <button
                      type="button"
                      onClick={() => referenceInputRef.current?.click()}
                      className="w-full rounded-2xl border border-dashed border-border bg-muted/30 p-4 text-left hover:bg-muted/50 transition-colors"
                      disabled={controlsDisabled || savingReferenceImage || clearingReferenceImage}
                    >
                      <div className="grid grid-cols-[96px_1fr] gap-4 items-center">
                        <div className="relative aspect-[4/5] max-h-[140px] rounded-xl bg-background border border-border/60 overflow-hidden flex items-center justify-center">
                          {(spot.reference_image || selectedReferencePreview) && (
                            <button
                              type="button"
                              onClick={(e) => {
                                e.preventDefault();
                                e.stopPropagation();
                                handleClearSpotReferenceImage();
                              }}
                              disabled={controlsDisabled || savingReferenceImage || clearingReferenceImage}
                              className="absolute right-2 top-2 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full bg-black/65 text-white hover:bg-black/80 disabled:opacity-50"
                            >
                              ×
                            </button>
                          )}
                          {selectedReferencePreview ? (
                            <img src={selectedReferencePreview} alt="selected" className="w-full h-full object-contain" />
                          ) : spot.reference_image ? (
                            <img
                              src={withAssetVersion(spot.reference_image, spot.updated_at)}
                              alt={spot.name}
                              className="w-full h-full object-contain"
                            />
                          ) : (
                            <div className="flex flex-col items-center gap-2 text-muted-foreground">
                              <ImagePlus className="w-7 h-7" />
                              <span className="text-[11px]">上传参考图</span>
                            </div>
                          )}
                        </div>
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm font-medium">
                            <UploadCloud className="w-4 h-4" />
                            上传{spotLabel}参考图
                          </div>
                          <p className="text-sm text-muted-foreground leading-6">
                            这张图会作为{spotLabel}图片编辑时的场景参考。建议当前区域清晰完整，画面别被其他大物体挡住。
                          </p>
                          {referenceImage && (
                            <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-3 py-1 text-xs text-primary">
                              已选择：{referenceImage.name}
                            </div>
                          )}
                          {savingReferenceImage && (
                            <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-3 py-1 text-xs text-primary">
                              正在保存参考图...
                            </div>
                          )}
                        </div>
                      </div>
                    </button>
                  </div>

                  <div className="space-y-2">
                    <label className="text-sm font-medium">{spotLabel}介绍</label>
                    <div className="space-y-3">
                      <div>
                        <div className="mb-2 text-xs font-medium text-foreground">直接测试模板</div>
                        <div className="flex flex-wrap gap-2">
                          {storeVisitDirectIntroTemplates.map((template) => {
                            const active = template.key === selectedIntroTemplateKey;
                            return (
                              <button
                                key={template.key}
                                type="button"
                                onClick={() => setSelectedIntroTemplateKey(template.key)}
                                className={`rounded-full border px-3 py-1.5 text-xs transition-colors ${
                                  active
                                    ? "border-primary bg-primary/10 text-primary"
                                    : "border-border text-muted-foreground hover:bg-accent"
                                }`}
                              >
                                {template.label}
                              </button>
                            );
                          })}
                        </div>
                      </div>
                      <div>
                        <div className="mb-2 text-xs font-medium text-foreground">可修改骨架模板</div>
                        <div className="flex flex-wrap gap-2">
                          {storeVisitEditableIntroTemplates.map((template) => {
                            const active = template.key === selectedIntroTemplateKey;
                            return (
                              <button
                                key={template.key}
                                type="button"
                                onClick={() => setSelectedIntroTemplateKey(template.key)}
                                className={`rounded-full border px-3 py-1.5 text-xs transition-colors ${
                                  active
                                    ? "border-primary bg-primary/10 text-primary"
                                    : "border-border text-muted-foreground hover:bg-accent"
                                }`}
                              >
                                {template.label}
                              </button>
                            );
                          })}
                        </div>
                      </div>
                    </div>
                    <div className="rounded-xl border border-border bg-muted/10 p-3 text-xs text-muted-foreground leading-6">
                      <div className="font-medium text-foreground mb-1">当前模板：{selectedIntroTemplate.label}</div>
                      <div>{selectedIntroTemplate.description}</div>
                      <div className="mt-2 text-[11px] leading-5 text-muted-foreground">{storeVisitPromptGuidance}</div>
                        <button
                          type="button"
                          onClick={() => setIntroText(selectedIntroTemplate.content)}
                        disabled={controlsDisabled}
                        className="mt-3 inline-flex items-center gap-2 rounded-md border border-border px-3 py-1.5 text-xs text-foreground hover:bg-accent transition-colors disabled:opacity-50"
                      >
                        套用这个模板
                      </button>
                    </div>
                    <Textarea
                      value={introText}
                      onChange={(e) => setIntroText(e.target.value)}
                      placeholder={`建议只写简短的“介绍内容”，例如这一区域最值得说的卖点、环境感觉或推荐理由。LLM 会自动生成自然的${spotLabel}介绍口播和动作节拍。`}
                      disabled={controlsDisabled}
                      className="min-h-[220px]"
                    />
                  </div>

                  <div className="flex items-center justify-between gap-3">
                    <p className="text-xs text-muted-foreground">建议只写短摘要，不要直接写成长台词稿。区域参考图上传后会立即保存；这里只需要手动保存介绍内容和提示词。</p>
                    <button
                      onClick={() => saveSpotContent(true)}
                      disabled={controlsDisabled || savingContent}
                      className="inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50 whitespace-nowrap"
                    >
                      <Save className="w-4 h-4" />
                      {savingContent ? "保存中..." : `保存${spotLabel}内容`}
                    </button>
                  </div>
                </div>
              </div>

              <div className="space-y-4">
                <button
                  type="button"
                  onClick={() =>
                    spot.generated_image &&
                    setPreviewState({
                      kind: "image",
                      title: `预览${spotLabel}图片`,
                      url: spot.generated_image,
                      version: spot.updated_at,
                    })
                  }
                  disabled={!spot.generated_image}
                  className="w-full rounded-2xl border border-border bg-muted/10 p-3 text-left hover:bg-muted/20 transition-colors disabled:opacity-100 disabled:hover:bg-muted/10"
                >
                  <div className="flex items-center justify-between">
                      <div className="text-sm font-medium">{spotLabel}图片</div>
                    {spot.image_generated_workflow && (
                      <div className="text-[10px] text-muted-foreground max-w-[110px] truncate">{spot.image_generated_workflow}</div>
                    )}
                  </div>
                  <div className="mt-3 aspect-[4/5] max-h-[180px] rounded-xl overflow-hidden bg-muted/30 flex items-center justify-center">
                    {imageReady ? (
                      <img
                        src={withAssetVersion(spot.generated_image, spot.updated_at)}
                        alt={`${spotLabel}图片`}
                        className="w-full h-full object-contain"
                      />
                    ) : (
                      <div className="text-xs text-muted-foreground">还没有生成{spotLabel}图片</div>
                    )}
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground">{imageReady ? "点击预览大图" : "生成后可点击预览"}</div>
                  {spot.image_last_error && <div className="mt-2 text-xs text-destructive break-all">{spot.image_last_error}</div>}
                </button>

                <button
                  type="button"
                  onClick={() =>
                    spot.generated_video &&
                    setPreviewState({
                      kind: "video",
                      title: `预览${spotLabel}视频`,
                      url: spot.generated_video,
                      version: spot.updated_at,
                    })
                  }
                  disabled={!spot.generated_video}
                  className="w-full rounded-2xl border border-border bg-muted/10 p-3 text-left hover:bg-muted/20 transition-colors disabled:opacity-100 disabled:hover:bg-muted/10"
                >
                  <div className="flex items-center justify-between">
                      <div className="text-sm font-medium">{spotLabel}视频</div>
                    {spot.video_generated_workflow && (
                      <div className="text-[10px] text-muted-foreground max-w-[110px] truncate">{spot.video_generated_workflow}</div>
                    )}
                  </div>
                  <div className="mt-3 aspect-[9/16] max-h-[220px] rounded-xl overflow-hidden bg-muted/30 flex items-center justify-center">
                    {videoReady ? (
                      <video
                        src={withAssetVersion(spot.generated_video, spot.updated_at)}
                        className="w-full h-full object-contain"
                        muted
                        playsInline
                      />
                    ) : (
                      <div className="flex flex-col items-center gap-2 text-muted-foreground">
                        <Play className="w-7 h-7" />
                        <span className="text-xs">还没有生成{spotLabel}视频</span>
                      </div>
                    )}
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground">
                    {videoReady ? "点击预览视频" : videoPositivePrompt.trim() ? "已具备生成视频条件" : "请先填写视频提示词或先使用项目级一键生成提示词"}
                  </div>
                  {spot.video_last_error && <div className="mt-2 text-xs text-destructive break-all">{spot.video_last_error}</div>}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      <Dialog
        open={projectGenerateDialogOpen}
        onOpenChange={(open) => {
          setProjectGenerateDialogOpen(open);
          if (!open) {
            setProjectGenerateRequestError("");
            setProjectGenerateMissingRefs([]);
            setProjectGenerateMissingDishGeneration(false);
          }
        }}
      >
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>{projectGenerateMode === "prompts" ? "项目一键生成提示词" : "项目一键生成"}</DialogTitle>
            <DialogDescription>
              {projectGenerateMode === "prompts"
                ? "这里填写整项目总文案。系统会一次调用 LLM 拆出门头、大厅、包间、厨房、特色区域、整桌菜品、优惠信息 7 个区域的介绍内容和视频提示词，只写回提示词，不自动提交图片和视频生成。"
                : "这里填写整项目总文案。系统会一次调用 LLM 拆出门头、大厅、包间、厨房、特色区域、整桌菜品、优惠信息 7 个区域的介绍内容和视频提示词，再按顺序批量跑图片、视频和菜品生成。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="rounded-2xl border border-border bg-muted/10 p-4 text-sm leading-6 text-muted-foreground">
              {projectGenerateMode === "prompts"
                ? "这次只会统一生成并写回各区域的介绍内容与视频提示词，不会自动提交图片、视频或菜品生成。"
                : "已经生成过图片或视频的区域会自动跳过，除非你先点了对应区域的“重置状态”。缺少参考图的区域这次只会跳过生成，不会阻塞 LLM 全量返回。"}
            </div>

            {projectGenerateRequestError && (
              <div className="rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
                {projectGenerateRequestError}
              </div>
            )}

            {projectGenerateMode === "full" && (projectGenerateMissingRefs.length > 0 || projectGenerateMissingDishGeneration) && (
              <div className="space-y-3 rounded-2xl border border-amber-500/30 bg-amber-500/5 p-4 text-sm">
                <div className="font-medium text-foreground">以下内容缺少参考图或还没配置 key frame</div>
                {projectGenerateMissingRefs.length > 0 && (
                  <div className="space-y-2">
                    <div className="text-muted-foreground">这些区域没有参考图，因此本次不会生成图片和视频：</div>
                    <div className="flex flex-wrap gap-2">
                      {projectGenerateMissingRefs.map((item) => (
                        <span key={`${item.spot_id}-${item.spot_type}`} className="rounded-full border border-border bg-background px-3 py-1 text-xs">
                          {item.name}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
                {projectGenerateMissingDishGeneration && (
                  <div className="text-muted-foreground">
                    `菜品生成` 还没有可运行的 key frame 组，本次会自动跳过这部分视频生成。
                  </div>
                )}
                <div className="text-muted-foreground">
                  如果你确认要继续，系统仍会全量生成 7 个区域的介绍内容和视频提示词，只是缺图部分会跳过渲染。
                </div>
              </div>
            )}

            <div className="space-y-2">
              <label className="text-sm font-medium">项目总文案</label>
              <Textarea
                value={projectGenerateContent}
                onChange={(e) => setProjectGenerateContent(e.target.value)}
                className="min-h-[260px]"
                placeholder={defaultStoreVisitAutoGenerateTemplate}
              />
            </div>
          </div>

          <DialogFooter className="gap-2">
            <button
              type="button"
              onClick={() => setProjectGenerateDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              取消
            </button>
            {projectGenerateMode === "full" && (projectGenerateMissingRefs.length > 0 || projectGenerateMissingDishGeneration) && (
              <button
                type="button"
                onClick={() => submitProjectAutoGenerate(true)}
                disabled={projectGenerateSubmitting}
                className="px-4 py-2 rounded-md border border-primary/30 text-primary hover:bg-primary/10 transition-colors disabled:opacity-50"
              >
                {projectGenerateSubmitting ? "继续提交中..." : "忽略缺失继续"}
              </button>
            )}
            <button
              type="button"
              onClick={() => submitProjectAutoGenerate(false)}
              disabled={projectGenerateSubmitting}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {projectGenerateSubmitting ? "提交中..." : projectGenerateMode === "prompts" ? "开始生成提示词" : "检查并开始生成"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={projectGenerateProgressOpen} onOpenChange={setProjectGenerateProgressOpen}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle>{projectGenerateMode === "prompts" ? "项目提示词实时流" : "项目一键生成实时流"}</DialogTitle>
            <DialogDescription>
              {projectGenerateMode === "prompts"
                ? "这里显示项目级 LLM 一次性拆解全部区域时的实时返回内容。返回完成后，只会写回介绍内容和视频提示词，不会自动提交图片或视频生成。"
                : "这里显示项目级 LLM 一次性拆解全部区域时的实时返回内容。返回完成后，系统会继续自动批量提交图片、视频和菜品生成任务。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3 text-sm">
              <div className="rounded-full bg-muted px-3 py-1 text-muted-foreground">
                任务状态：{projectGenerateTask?.status || "idle"}
              </div>
              {projectGenerateTask?.progress !== undefined && (
                <div className="rounded-full bg-muted px-3 py-1 text-muted-foreground">进度：{projectGenerateTask.progress}%</div>
              )}
              {projectGenerateTaskId && (
                <div className="rounded-full bg-muted px-3 py-1 text-muted-foreground">任务 ID：{projectGenerateTaskId}</div>
              )}
              {projectGenerateTaskStream?.provider_name && (
                <div className="rounded-full bg-muted px-3 py-1 text-muted-foreground">
                  引擎：{projectGenerateTaskStream.provider_name}
                </div>
              )}
            </div>

            <div
              ref={projectGenerateStreamRef}
              className="min-h-[360px] max-h-[55vh] overflow-auto rounded-2xl border border-border bg-muted/10 p-4 text-sm leading-6 whitespace-pre-wrap break-words font-mono"
            >
              {projectGenerateTaskStream?.content?.trim()
                ? projectGenerateTaskStream.content
                : projectGenerateTask?.status === "completed"
                  ? "已完成，但暂时没有实时流内容。"
                  : projectGenerateTaskActive
                    ? "正在等待 LLM 开始返回内容..."
                    : "等待任务开始。"}
            </div>

            {projectGenerateTask?.error && (
              <div className="rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
                {projectGenerateTask.error}
              </div>
            )}
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => setProjectGenerateProgressOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              关闭
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={promptDialogOpen} onOpenChange={setPromptDialogOpen}>
        <DialogContent className="max-w-5xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>修改固定提示词</DialogTitle>
            <DialogDescription>
              当前支持直接手动填写，或先使用项目级一键生成提示词批量写回后，再在这里做人工微调。
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-6 xl:grid-cols-2">
            <div className="space-y-4">
              <h3 className="text-lg font-semibold">图片提示词</h3>
              <div className="space-y-2">
                <label className="text-sm font-medium">正向提示词</label>
                <Textarea
                  value={imagePositivePrompt}
                  onChange={(e) => setImagePositivePrompt(e.target.value)}
                  className="min-h-[260px]"
                />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">负向提示词</label>
                <Textarea
                  value={imageNegativePrompt}
                  onChange={(e) => setImageNegativePrompt(e.target.value)}
                  className="min-h-[160px]"
                />
              </div>
            </div>

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold">视频提示词</h3>
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <span>时长</span>
                  <Input
                    value={videoDurationSeconds}
                    onChange={(e) => setVideoDurationSeconds(e.target.value)}
                    className="w-20 h-9"
                  />
                  <span>秒</span>
                </div>
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">正向提示词</label>
                <Textarea
                  value={videoPositivePrompt}
                  onChange={(e) => setVideoPositivePrompt(e.target.value)}
                  className="min-h-[320px]"
                  placeholder="可留空，后续可手动填写，或先使用项目级一键生成提示词批量生成。"
                />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">负向提示词</label>
                <Textarea
                  value={videoNegativePrompt}
                  onChange={(e) => setVideoNegativePrompt(e.target.value)}
                  className="min-h-[120px]"
                />
              </div>
            </div>
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => setPromptDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={savePromptChanges}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              保存提示词
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={projectResetDialogOpen}
        onOpenChange={(open) => {
          setProjectResetDialogOpen(open);
          if (!open && !resettingProjectMode) {
            setProjectResetMode(null);
          }
        }}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {projectResetMode === "images" ? "确认一键重置图片" : projectResetMode === "videos" ? "确认一键重置视频" : "确认一键重置所有"}
            </DialogTitle>
            <DialogDescription>
              {projectResetMode === "images"
                ? "这会清空项目里所有普通区域的生成图片状态与图片文件。已有视频不会删除。"
                : projectResetMode === "videos"
                  ? "这会清空项目里所有普通区域的视频状态与视频文件。菜品生成条目不会受影响。"
                  : "这会清空项目里所有普通区域的图片与视频状态，并清空菜品生成条目的视频结果。"}
            </DialogDescription>
          </DialogHeader>

          <div className="rounded-xl border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-muted-foreground">
            这个操作会直接重置当前项目的生成结果。确认后需要你重新发起对应的生成任务。
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => {
                setProjectResetDialogOpen(false);
                if (!resettingProjectMode) {
                  setProjectResetMode(null);
                }
              }}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleProjectReset}
              disabled={!projectResetMode || !!resettingProjectMode}
              className="px-4 py-2 rounded-md bg-destructive text-destructive-foreground hover:bg-destructive/90 transition-colors disabled:opacity-50"
            >
              {resettingProjectMode ? "重置中..." : "确认重置"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={resolutionDialogOpen} onOpenChange={setResolutionDialogOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>修改视频分辨率</DialogTitle>
            <DialogDescription>这里修改后的宽高会同时用于 LTX2.3 视频生成和视频提示词反推参考。</DialogDescription>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">宽度</label>
              <Input value={videoWidth} onChange={(e) => setVideoWidth(e.target.value)} />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">高度</label>
              <Input value={videoHeight} onChange={(e) => setVideoHeight(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setResolutionDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={async () => {
                const saved = await saveSpotContent(false);
                if (saved) {
                  setResolutionDialogOpen(false);
                  toast.success("视频分辨率已更新");
                }
              }}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              保存分辨率
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={dishGenerationDialogOpen}
          onOpenChange={(open) => {
            setDishGenerationDialogOpen(open);
            if (!open) {
              resetDishGenerationDraft();
            }
          }}
      >
        <DialogContent className="max-w-5xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editingDishGenerationItemId ? "编辑菜品生成参数" : "新增一组 key frame"}</DialogTitle>
            <DialogDescription>
              {editingDishGenerationItemId
                ? "这里可以修改当前这组的 motion preset、逐段时长和逐段提示词，也可以继续追加新的 key frame 图片。"
                : "先上传任意数量的 key frame 图，再按逐段时长和逐段提示词生成一条纯菜品视频。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">key frame 图片</label>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={addDishGenerationFrameSlot}
                    className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-accent transition-colors"
                  >
                    新增图片
                  </button>
                </div>
              </div>
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                {dishGenerationExistingFrames.map((frame, index) => (
                  <div key={`existing-frame-${index}`} className="space-y-2 rounded-xl border border-border bg-muted/10 p-3">
                    <div className="text-sm font-medium">Frame {index + 1}</div>
                    <div className="aspect-square overflow-hidden rounded-lg bg-muted/20">
                      <img src={withAssetVersion(frame)} alt={`Frame ${index + 1}`} className="h-full w-full object-cover" />
                    </div>
                    <div className="text-xs text-muted-foreground">已保存图片</div>
                  </div>
                ))}
                {dishGenerationFrameFiles.map((file, index) => {
                  const totalSlots = dishGenerationExistingFrames.length + dishGenerationFrameFiles.length;
                  const displayIndex = dishGenerationExistingFrames.length + index;
                  return (
                    <div key={`dish-frame-input-${displayIndex}`} className="space-y-2 rounded-xl border border-border bg-muted/10 p-3">
                      <div className="flex items-center justify-between gap-2">
                        <div className="text-sm font-medium">Frame {displayIndex + 1}</div>
                        {index === dishGenerationFrameFiles.length - 1 && totalSlots > 2 && (
                          <button
                            type="button"
                            onClick={() => removeDishGenerationFrameSlot(index)}
                            className="text-xs text-destructive hover:underline"
                          >
                            删除
                          </button>
                        )}
                      </div>
                      <input
                        type="file"
                        accept="image/*"
                        onChange={(e) => updateDishGenerationFrameFile(index, e.target.files?.[0] || null)}
                        className="block w-full text-sm text-muted-foreground file:mr-3 file:rounded-md file:border-0 file:bg-primary/10 file:px-3 file:py-2 file:text-primary"
                      />
                      <div className="text-xs text-muted-foreground truncate">{file?.name || "未选择任何文件"}</div>
                    </div>
                  );
                })}
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_220px]">
              <div className="space-y-2">
                <label className="text-sm font-medium">motion preset</label>
                <select
                  value={dishGenerationPresetKey}
                  onChange={(e) => handleDishGenerationPresetChange(e.target.value)}
                  className="h-10 w-full rounded-md border border-border bg-background px-3 text-sm"
                >
                  {storeVisitDishMotionPresets.map((preset) => (
                    <option key={preset.key} value={preset.key}>
                      {preset.label}
                    </option>
                  ))}
                </select>
                <div className="text-xs text-muted-foreground">
                  {getStoreVisitDishMotionPreset(dishGenerationPresetKey).description}
                </div>
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">当前总览</label>
                <div className="rounded-xl border border-border bg-muted/10 px-3 py-3 text-sm leading-6 text-muted-foreground">
                  图片数：<span className="font-medium text-foreground">{dishGenerationExistingFrames.length + dishGenerationFrameFiles.length}</span>
                  <br />
                  过渡段：<span className="font-medium text-foreground">{dishGenerationSegments.length}</span>
                  <br />
                  总时长约：
                  <span className="ml-1 font-medium text-foreground">
                    {dishGenerationSegments
                      .reduce((sum, segment) => sum + Math.max(0.1, Number.parseFloat(segment.durationSeconds) || 2), 0)
                      .toFixed(1)}
                    秒
                  </span>
                </div>
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              {dishGenerationSegments.map((segment, index) => (
                <div key={`dish-prompt-${index}`} className="space-y-2 rounded-xl border border-border bg-muted/10 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-sm font-medium">第 {index + 1} 段提示词</div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <span>时长</span>
                      <Input
                        value={segment.durationSeconds}
                        onChange={(e) => updateDishGenerationSegmentDuration(index, e.target.value)}
                        className="h-8 w-20"
                      />
                      <span>秒</span>
                    </div>
                  </div>
                  <Textarea
                    value={segment.prompt}
                    onChange={(e) => updateDishGenerationSegmentPrompt(index, e.target.value)}
                    className="min-h-[120px]"
                  />
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => setDishGenerationDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={editingDishGenerationItemId ? handleUpdateDishGenerationItem : handleCreateDishGenerationItem}
              disabled={creatingDishGenerationItem}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {creatingDishGenerationItem ? (editingDishGenerationItemId ? "保存中..." : "新增中...") : (editingDishGenerationItemId ? "保存修改" : "新增这一组")}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={previewState !== null} onOpenChange={(open) => !open && setPreviewState(null)}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>{previewState?.title || "预览媒体"}</DialogTitle>
          </DialogHeader>
          {previewState?.kind === "image" && previewState.url && (
            <div className="max-h-[75vh] overflow-auto rounded-xl bg-muted/20 p-3">
              <img
                src={withAssetVersion(previewState.url, previewState.version)}
                alt={previewState.title}
                className="mx-auto max-h-[70vh] w-auto object-contain rounded-lg"
              />
            </div>
          )}
          {previewState?.kind === "video" && previewState.url && (
            <div className="rounded-xl bg-muted/20 p-3">
              <video
                src={withAssetVersion(previewState.url, previewState.version)}
                controls
                autoPlay
                className="mx-auto max-h-[70vh] w-auto object-contain rounded-lg"
              />
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
